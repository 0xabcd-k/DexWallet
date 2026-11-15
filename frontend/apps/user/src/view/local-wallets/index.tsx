import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Button } from 'antd-mobile';
import { App } from '@capacitor/app';
import { useTranslation } from '@dexwallet/i18n';
import { RequireAuth, useAuth } from '../../auth/context';
import { nativeVault, nativeCatalog, type NativeWalletVault, type PrivacyState } from '../../app/native/local-wallet/bridge';
import { createWalletRepository, type CatalogPort, type ListedWallet, type WalletRecord } from '../../app/native/local-wallet/repository';
import { createMaterial, deriveAccounts, importMaterial, normalizeWords, WalletError, walletErrorCode, type WalletMaterial, type WalletAccount, type MnemonicScheme, type TonVersion } from '../../app/native/local-wallet/crypto';
import './style.less';

export default function LocalWallets() {
  const auth = useAuth();
  return <RequireAuth><WalletManager key={auth.session.credential?.idx} userIdx={auth.session.credential?.idx ?? ''}/></RequireAuth>;
}
interface Preview { material: WalletMaterial; accounts: WalletAccount[] }
interface Backup extends Preview { record: WalletRecord; phase: 'reveal' | 'check'; positions: number[] }
export function WalletManager({ userIdx, vault = nativeVault, catalog = nativeCatalog }: { userIdx: string; vault?: NativeWalletVault; catalog?: CatalogPort }) {
  const { t } = useTranslation();
  const [token] = useState(() => crypto.randomUUID());
  const repository = useMemo(() => createWalletRepository(userIdx, token, catalog, vault), [userIdx, token, catalog, vault]);
  const [privacy, setPrivacy] = useState<PrivacyState>();
  const [wallets, setWallets] = useState<ListedWallet[]>([]);
  const [mode, setMode] = useState<'list' | 'create' | 'import'>('list');
  const [name, setName] = useState('');
  const [words, setWords] = useState('');
  const [scheme, setScheme] = useState<MnemonicScheme>('bip39');
  const [tonVersion, setTonVersion] = useState<TonVersion>('v5r1');
  const [preview, setPreview] = useState<Preview>();
  const [backup, setBackup] = useState<Backup>();
  const [answers, setAnswers] = useState<string[]>([]);
  const [removal, setRemoval] = useState<WalletRecord>();
  const [removeAccepted, setRemoveAccepted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const lifetime = useRef({ mounted: false, active: false, epoch: 0, busy: false });
  const hideSecrets = useCallback(() => {
    lifetime.current.epoch++; setWords(''); setPreview(undefined); setBackup(undefined); setAnswers([]);
  }, []);
  const refresh = useCallback(async () => {
    const items = await repository.list();
    if (lifetime.current.mounted) setWallets(items);
  }, [repository]);
  useEffect(() => {
    const life = lifetime.current; life.mounted = true;
    let active = true;
    let listener: Awaited<ReturnType<NativeWalletVault['addListener']>> | undefined;
    const changed = (state: PrivacyState) => {
      if (!active || state.token !== token) return;
      life.active = state.active; hideSecrets(); setPrivacy(state);
    };
    const visible = () => { if (document.visibilityState === 'hidden') hideSecrets(); };
    document.addEventListener('visibilitychange', visible);
    void (async () => {
      const handle = await vault.addListener('privacyChange', changed);
      if (!active) { await handle.remove(); return; }
      listener = handle;
      const state = await vault.protect({ token });
      if (!active) return;
      changed(state); await refresh();
    })().catch(e => { if (active) setError(walletErrorCode(e)); });
    return () => {
      active = false; life.mounted = false; life.active = false; life.epoch++;
      document.removeEventListener('visibilitychange', visible);
      void listener?.remove();
      void vault.unprotect({ token }).catch(() => {});
    };
  }, [vault, token, refresh, hideSecrets]);
  useLayoutEffect(() => {
    if (privacy?.active) void vault.acknowledge({ token, revision: privacy.revision }).catch(e => setError(walletErrorCode(e)));
  }, [privacy, token, vault]);
  async function run(action: () => Promise<void>) {
    if (lifetime.current.busy) return;
    lifetime.current.busy = true; setBusy(true); setError(undefined); setNotice(undefined);
    try { await action(); }
    catch (e) { if (lifetime.current.mounted) setError(walletErrorCode(e)); }
    finally {
      if (lifetime.current.mounted) {
        try { await refresh(); } catch (e) { setError(walletErrorCode(e)); }
        setBusy(false);
      }
      lifetime.current.busy = false;
    }
  }
  function shown(epoch: number) {
    return lifetime.current.mounted && lifetime.current.active && lifetime.current.epoch === epoch && document.visibilityState !== 'hidden';
  }
  async function save(material: WalletMaterial, accounts: WalletAccount[]) {
    await repository.save(name, material, accounts);
    if (lifetime.current.mounted) { hideSecrets(); setMode('list'); setName(''); setNotice('localSaved'); }
  }
  async function create() {
    const epoch = lifetime.current.epoch;
    const material = createMaterial(), accounts = await deriveAccounts(material);
    if (!shown(epoch)) throw new WalletError('LOCAL_VIEW_CLOSED');
    await save(material, accounts);
  }
  async function reviewImport() {
    const epoch = lifetime.current.epoch;
    const material = await importMaterial(words, scheme, tonVersion), accounts = await deriveAccounts(material);
    if (!shown(epoch)) throw new WalletError('LOCAL_VIEW_CLOSED');
    setPreview({ material, accounts }); setWords('');
  }
  async function unlock(record: WalletRecord, reveal: boolean) {
    let epoch = -1;
    const data = await repository.unlock(record, () => {
      epoch = lifetime.current.epoch;
      if (!shown(epoch)) throw new WalletError('LOCAL_VIEW_CLOSED');
    });
    const state = await App.getState();
    if (!state.isActive || !shown(epoch)) throw new WalletError('LOCAL_VIEW_CLOSED');
    if (reveal) setBackup({ ...data, record, phase: 'reveal', positions: [] });
    else setNotice('localRecovered');
  }
  function startCheck() {
    if (!backup) return;
    const total = backup.material.words.split(' ').length, selected = new Set<number>();
    while (selected.size < 3) selected.add(crypto.getRandomValues(new Uint32Array(1))[0]! % total);
    setAnswers([]); setBackup({ ...backup, phase: 'check', positions: [...selected].sort((a, b) => a - b) });
  }
  async function confirmBackup() {
    if (!backup) return;
    const expected = backup.material.words.split(' ');
    if (!backup.positions.every((position, i) => normalizeWords(answers[i] ?? '') === expected[position])) throw new WalletError('LOCAL_BACKUP_MISMATCH');
    await repository.confirmBackup(backup.record);
    hideSecrets(); setNotice('localBackupDone');
  }
  const protectedView = !!privacy?.active;
  const errorKey = error && localErrorKey(error);
  return <section className="local-wallets"><h1>{t('localWallets')}</h1><p className="muted">{t('localScope')}</p>
    {errorKey && <p role="alert">{t(errorKey)}</p>}{notice && <p role="status">{t(notice)}</p>}
    {!privacy && <p role="status">{t('localProtecting')}</p>}
    {protectedView && <>
      {backup ? <section className="wallet-panel" aria-label={t('localBackup')}>
        <h2>{backup.record.name} · {t('localBackup')}</h2><p>{t('localBackupWarning')}</p>
        <p>{t(backup.material.scheme === 'bip39' ? 'localBip39' : 'localTonNative')} · TON {backup.material.tonVersion.toUpperCase()}</p>
        {backup.phase === 'reveal' ? <><ol className="mnemonic-words" data-private="true" onCopy={e => e.preventDefault()}>{backup.material.words.split(' ').map((word, i) => <li key={i}>{word}</li>)}</ol><Button disabled={busy} onClick={startCheck}>{t('localStartCheck')}</Button></> :
          <form onSubmit={e => { e.preventDefault(); void run(confirmBackup); }} autoComplete="off"><p>{t('localCheckHint')}</p>{backup.positions.map((position, i) => <label key={position}>{t('localWordNumber', { number: position + 1 })}<input autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={answers[i] ?? ''} onChange={e => setAnswers(a => { const next = [...a]; next[i] = e.target.value; return next; })}/></label>)}<Button type="submit" disabled={busy || answers.filter(Boolean).length !== 3}>{t('localConfirmBackup')}</Button></form>}
        <Button disabled={busy} onClick={hideSecrets}>{t('localHide')}</Button>
      </section> : mode !== 'list' ? <section className="wallet-panel">
        <h2>{t(mode === 'create' ? 'localCreate' : 'localImport')}</h2>
        <label>{t('localName')}<input maxLength={40} value={name} onChange={e => setName(e.target.value)} disabled={busy}/></label>
        {mode === 'create' ? <><p>{t('localCreateHint')}</p><Button disabled={busy || !name.trim()} onClick={() => { void run(create); }}>{t('localCreateSave')}</Button></> : preview ? <>
          <p>{t('localPreviewHint')}</p><Accounts accounts={preview.accounts}/><Button disabled={busy || !name.trim()} onClick={() => { const data = preview; void run(() => save(data.material, data.accounts)); }}>{t('localConfirmImport')}</Button>
          <Button disabled={busy} onClick={hideSecrets}>{t('localEditImport')}</Button>
        </> : <form autoComplete="off" onSubmit={e => { e.preventDefault(); void run(reviewImport); }}>
          <p>{t('localImportLimits')}</p>
          <label>{t('localScheme')}<select value={scheme} disabled={busy} onChange={e => setScheme(e.target.value === 'ton' ? 'ton' : 'bip39')}><option value="bip39">{t('localBip39')}</option><option value="ton">{t('localTonNative')}</option></select></label>
          <label>{t('localTonVersion')}<select value={tonVersion} disabled={busy} onChange={e => setTonVersion(e.target.value === 'v4r2' ? 'v4r2' : 'v5r1')}><option value="v5r1">V5R1</option><option value="v4r2">V4R2</option></select></label>
          <label>{t('localMnemonic')}<textarea rows={5} maxLength={512} autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={words} onChange={e => setWords(e.target.value)} disabled={busy}/></label>
          <Button type="submit" disabled={busy || !words.trim() || !name.trim()}>{t('localPreview')}</Button>
        </form>}
        <Button disabled={busy} onClick={() => { hideSecrets(); setMode('list'); }}>{t('localCancel')}</Button>
      </section> : <>
        <div className="local-actions"><Button disabled={busy} onClick={() => { setMode('create'); setName(''); setNotice(undefined); }}>{t('localCreate')}</Button><Button disabled={busy} onClick={() => { setMode('import'); setName(''); setNotice(undefined); }}>{t('localImport')}</Button></div>
        {!wallets.length && <p>{t('localEmpty')}</p>}
        {wallets.map(record => <article className="wallet-panel" key={record.walletId}><h2>{record.name}</h2>
          <p>{t(!record.available ? 'localMissing' : record.state === 'pending' ? 'localPending' : record.backedUp ? 'localBackedUp' : 'localNeedsBackup')}</p>
          <p>{t(record.scheme === 'bip39' ? 'localBip39' : 'localTonNative')} · TON {record.tonVersion.toUpperCase()}</p><Accounts accounts={record.accounts}/>
          <div className="local-actions">{record.available && <Button disabled={busy} onClick={() => { void run(() => unlock(record, record.state === 'ready')); }}>{t(record.state === 'pending' ? 'localRecover' : 'localBackup')}</Button>}
            <Button disabled={busy} onClick={() => { setRemoval(record); setRemoveAccepted(false); }}>{t('localRemove')}</Button></div>
          {removal?.walletId === record.walletId && <div className="remove-wallet"><p>{t('localRemoveWarning')}</p><label><input type="checkbox" checked={removeAccepted} onChange={e => setRemoveAccepted(e.target.checked)}/>{t('localRemoveAccept')}</label>
            <Button disabled={busy || !removeAccepted} onClick={() => { void run(async () => { await repository.remove(record); if (lifetime.current.mounted) { setRemoval(undefined); setNotice('localRemoved'); } }); }}>{t('localConfirmRemove')}</Button><Button disabled={busy} onClick={() => setRemoval(undefined)}>{t('localCancel')}</Button></div>}
        </article>)}
        <Button disabled={busy} onClick={() => { void run(refresh); }}>{t('localRefresh')}</Button>
      </>}
    </>}
  </section>;
}
function Accounts({ accounts }: { accounts: WalletAccount[] }) {
  const { t } = useTranslation();
  return <dl className="local-addresses">{accounts.map(account => <div key={account.network}><dt>{t(`localNetwork_${account.network}`)}</dt><dd data-testid={`address-${account.network}`}>{account.address}</dd><dd className="muted">{account.path === 'ton-native' ? t('localTonDerivation') : account.path}</dd></div>)}</dl>;
}
export function localErrorKey(code: string): string {
  if (code === 'VAULT_CANCELLED') return 'localCancelled';
  if (code === 'VAULT_DEVICE_LOCK_REQUIRED') return 'localLockRequired';
  if (code === 'VAULT_KEY_UNAVAILABLE') return 'localKeyUnavailable';
  if (code === 'VAULT_CORRUPTED' || code === 'LOCAL_INVALID_SECRET') return 'localSecretDamaged';
  if (code === 'LOCAL_INVALID_MNEMONIC') return 'localInvalidMnemonic';
  if (code === 'LOCAL_DUPLICATE' || code === 'VAULT_EXISTS') return 'localDuplicate';
  if (code === 'LOCAL_BACKUP_MISMATCH') return 'localBackupMismatch';
  if (code === 'LOCAL_CATALOG_CORRUPTED') return 'localCatalogDamaged';
  if (code === 'LOCAL_VIEW_CLOSED' || code === 'VAULT_INTERRUPTED') return 'localClosed';
  if (code === 'LOCAL_NAME_REQUIRED') return 'localNameRequired';
  return 'localFailed';
}
