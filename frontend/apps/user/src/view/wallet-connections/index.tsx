import { Link } from 'react-router';
import { useEffect, useState, useSyncExternalStore } from 'react';
import { Button, Input } from 'antd-mobile';
import { AppLauncher } from '@capacitor/app-launcher';
import type { WalletInfoRemote } from '@tonconnect/sdk';
import { useTranslation } from '@dexwallet/i18n';
import { RequireAuth, useAuth } from '../../auth/context';
import { createTonConnection, type TonConnection } from '../../app/native/ton';
import './style.less';
export default function WalletConnections() {
  const auth = useAuth();
  return <RequireAuth><ConnectionPage key={auth.session.credential?.idx} userIdx={auth.session.credential?.idx ?? ''}/></RequireAuth>;
}
function ConnectionPage({ userIdx }: { userIdx: string }) {
  const { t } = useTranslation();
  const [connection, setConnection] = useState<TonConnection>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    if (!import.meta.env.TON_MANIFEST_URL) return;
    let c: TonConnection;
    try { c = createTonConnection(userIdx, import.meta.env.TON_MANIFEST_URL); setConnection(c); }
    catch (e) { setError(e); return; }
    return () => c.dispose();
  }, [userIdx]);
  return <section className="wallet-connections"><h1>{t('nativeWallets')}</h1><p>{t('nativeConnectionScope')}</p><p><Link to="/chain-assets">{t('chainAssets')}</Link></p>
    {!import.meta.env.TON_MANIFEST_URL && <p role="status">{t('nativeWalletUnavailable')}</p>}
    {connection && <ConnectedWallet connection={connection}/>}{error !== undefined && <p role="alert">{t('nativeConnectionFailed')}</p>}
  </section>;
}
function ConnectedWallet({ connection }: { connection: TonConnection }) {
  const { t } = useTranslation();
  const state = useSyncExternalStore(connection.subscribe, connection.getSnapshot);
  const [wallets, setWallets] = useState<WalletInfoRemote[]>([]);
  const [search, setSearch] = useState('');
  const [busy, setBusy] = useState(false);
  const [launchFailed, setLaunchFailed] = useState(false);
  const [error, setError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  useEffect(() => { let active = true; setError(undefined);
    void connection.wallets().then(w => { if (active) setWallets(w); }, e => { if (active) setError(e); });
    return () => { active = false; };
  }, [connection, revision]);
  async function connect(wallet: WalletInfoRemote) {
    setBusy(true); setError(undefined); setLaunchFailed(false);
    let launching = false;
    try { const url = await connection.connect(wallet); launching = true; const result = await AppLauncher.openUrl({ url }); if (!result.completed) throw new Error('Wallet launch failed'); }
    catch (e) {
      if (launching) setLaunchFailed(true); else setError(e);
      try { await connection.cancel(); } catch (cancelError) { setError(cancelError); }
    }
    finally { setBusy(false); }
  }
  async function disconnect() { setBusy(true); setError(undefined); try { await connection.disconnect(); } catch (e) { setError(e); } finally { setBusy(false); } }
  return <>
    {state.restoring && <p role="status">{t('nativeRestoring')}</p>}
    {state.wallet && <article className="task-card"><h2>{state.wallet.device.appName}</h2><p>{t('nativeConnected')}</p><dl><dt>{t('nativeAddress')}</dt><dd>{state.wallet.account.address}</dd><dt>{t('nativeNetwork')}</dt><dd>{state.wallet.account.chain === '-3' ? t('nativeTestnet') : state.wallet.account.chain === '-239' ? t('nativeMainnet') : state.wallet.account.chain}</dd></dl><Button disabled={busy} onClick={() => { void disconnect(); }}>{t('nativeDisconnect')}</Button></article>}
    {state.connecting && <p role="status">{t('nativeWaiting')}</p>}
    {state.connecting && <Button disabled={busy} onClick={() => { setBusy(true); void connection.cancel().catch(setError).finally(() => setBusy(false)); }}>{t('nativeCancel')}</Button>}
    <label htmlFor="wallet-search">{t('nativeSearch')}</label><Input id="wallet-search" value={search} onChange={setSearch}/>
    <ul>{wallets.filter(w => `${w.name} ${w.appName}`.toLowerCase().includes(search.toLowerCase())).map(w => <li key={w.appName}><span>{w.name}</span><Button disabled={busy || state.restoring || state.connecting} onClick={() => { void connect(w); }}>{t(state.wallet ? 'nativeSwitch' : 'nativeConnect')}</Button></li>)}</ul>
    {launchFailed && <p role="alert">{t('nativeLaunchFailed')}</p>}
    {error !== undefined && <p role="alert">{t('nativeConnectionFailed')}</p>} {state.error !== undefined && <p role="alert">{t('nativeConnectionFailed')}</p>}<Button disabled={busy} onClick={() => setRevision(n => n + 1)}>{t('nativeRefresh')}</Button>
  </>;
}
