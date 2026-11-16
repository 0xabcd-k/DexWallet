import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { Button, Input, Popup } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { ChainNetwork, ChainTransferPreviewData } from '@dexwallet/api-contract/platform';
import { Failure, RequireAuth, useAuth } from '../../auth/context';
import { CHAIN_RECEIVE_PREFIX, chainPreparationRoute, receiveFields } from '../../app/chain-intent';
import { useViewingWallets } from '../../app/native/chain-assets/use-wallets';
import { addressFor, type ViewingWallet } from '../../app/native/chain-assets/wallets';
import { atomicAmount, previewChainTransfer, resolveChainTarget } from '../../app/native/chain-transfer/api';
import { useChainCatalog, WalletSourceStatus } from './sources';
import { readPending } from '../transfers/pending';
import './style.less';

export default function ChainTransfer() {
  const auth = useAuth();
  return <RequireAuth><PreparationPage key={auth.session.credential?.idx} userIdx={auth.session.credential?.idx ?? ''}/></RequireAuth>;
}
function PreparationPage({ userIdx }: { userIdx: string }) {
  const { t } = useTranslation(), auth = useAuth(), [params] = useSearchParams();
  const source = useViewingWallets(userIdx), catalog = useChainCatalog();
  const [pending, setPending] = useState(false), [storageError, setStorageError] = useState(false);
  useEffect(() => { try { setPending(!!readPending(localStorage, `${auth.session.credential!.issuer}:${userIdx}`)); } catch { setStorageError(true); } }, [auth.session.credential, userIdx]);
  const invalid = [...params.keys()].some(k => k !== 'request') || params.getAll('request').length > 1;
  return <section className="transfer-page chain-prepare"><h1>{t('transferChain')}</h1><p>{t('chainPreparationScope')}</p>
    {pending || storageError ? <><p role="alert">{t(storageError ? 'paymentStorageUnavailable' : 'chainExistingPayment')}</p><Link to="/pay?manual=1">{t('recoverTransfer')}</Link></> : <>
      {catalog.loading && <p role="status">{t('common:loading')}</p>}
      {catalog.error !== undefined && <><Failure error={catalog.error}/><Button onClick={catalog.retry}>{t('common:retry')}</Button></>}
      {invalid ? <p role="alert">{t('invalidReceiveLink')}</p> : <PreparationForm key={params.get('request') ?? ''} networks={catalog.networks} wallets={source.wallets} request={params.get('request') ?? ''}/>}
      <WalletSourceStatus source={source} returnTo={`/chain-transfer${params.size ? `?${params}` : ''}`}/>
    </>}
  </section>;
}
export function PreparationForm({ networks, wallets, request, resolve = resolveChainTarget, preview = previewChainTransfer }: {
  networks: ChainNetwork[]; wallets: ViewingWallet[]; request: string; resolve?: typeof resolveChainTarget; preview?: typeof previewChainTransfer;
}) {
  const { t } = useTranslation();
  const isCode = request.startsWith(CHAIN_RECEIVE_PREFIX), code = receiveFields(request);
  const [networkID, setNetworkID] = useState(''), [assetID, setAssetID] = useState(''), [address, setAddress] = useState(isCode ? '' : request);
  const [amount, setAmount] = useState(''), [review, setReview] = useState<ChainTransferPreviewData>(), [walletStep, setWalletStep] = useState(false);
  const [walletID, setWalletID] = useState(''), [search, setSearch] = useState(''), [error, setError] = useState<unknown>(), [busy, setBusy] = useState(false);
  const [resolved, setResolved] = useState(!isCode), [revision, setRevision] = useState(0);
  const attempt = useRef<{ version: number; controller?: AbortController }>({ version: 0 });
  const network = networks.find(n => n.id === networkID), asset = network?.assets.find(a => a.id === assetID);
  const compatible = review ? wallets.filter(w => addressFor(w, network!)) : [];
  const wallet = compatible.find(w => w.id === walletID);
  function invalidate() { attempt.current.version++; attempt.current.controller?.abort(); setBusy(false); setReview(undefined); setWalletStep(false); setWalletID(''); setError(undefined); }
  useEffect(() => {
    if (!isCode || !networks.length || !code) return;
    let active = true; const controller = new AbortController(); setBusy(true); setResolved(false); setError(undefined);
    void resolve({ receiveCode: request }, networks, controller.signal).then(target => {
      if (active) { setNetworkID(target.network); setAssetID(target.assetId); setAddress(target.address); setResolved(true); }
    }, e => { if (active) setError(e); }).finally(() => { if (active) setBusy(false); });
    return () => { active = false; controller.abort(); };
  // The request is immutable for this keyed form. No repeated resolution on keystrokes.
  }, [request, isCode, networks, resolve, revision]);
  useEffect(() => () => { attempt.current.version++; attempt.current.controller?.abort(); }, []);
  async function prepare() {
    if (!network || !asset || busy) return;
    invalidate(); setBusy(true); const version = attempt.current.version, controller = new AbortController(); attempt.current.controller = controller;
    try { const result = await preview({ network: network.id, assetId: asset.id, address: address.trim(), amount }, networks, controller.signal); if (attempt.current.version === version) setReview(result); }
    catch (e) { if (attempt.current.version === version) setError(e); }
    finally { if (attempt.current.version === version) setBusy(false); }
  }
  const validAmount = !!asset && (atomicAmount(amount, asset.decimals) ?? 0n) > 0n;
  return <>
    {isCode && !code ? <p role="alert">{t('invalidReceiveLink')}</p> : <>
      {isCode && !resolved && error !== undefined && <Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button>}
      <form onSubmit={e => { e.preventDefault(); void prepare(); }}>
        <label htmlFor="chain-transfer-network">{t('nativeNetwork')}</label><select id="chain-transfer-network" value={networkID} disabled={isCode} onChange={e => { invalidate(); setNetworkID(e.target.value); setAssetID(''); }}><option value="">{t('chainChooseNetwork')}</option>{networks.map(n => <option key={n.id} value={n.id}>{t(`chainNetwork_${n.id}`)}</option>)}</select>
        {network?.testnet && <p className="chain-testnet">{t('chainTestnet')}</p>}
        <label htmlFor="chain-transfer-asset">{t('chainToken')}</label><select id="chain-transfer-asset" value={assetID} disabled={isCode || !network} onChange={e => { invalidate(); setAssetID(e.target.value); }}><option value="">{t('chainChooseAsset')}</option>{network?.assets.map(a => <option key={a.id} value={a.id}>{a.symbol}{a.origin === 'test_fixture' ? ` · ${t('chainFixture')}` : ''}</option>)}</select>
        {asset?.contract && <p className="chain-address">{t('chainIdentity')}: {asset.contract}</p>}
        <label htmlFor="chain-transfer-address">{t('chainRecipientAddress')}</label><Input id="chain-transfer-address" value={address} readOnly={isCode} onChange={v => { invalidate(); setAddress(v); }} maxLength={128}/>
        {isCode && <p>{t('chainTargetFixed')}</p>}
        <label htmlFor="chain-transfer-amount">{t('chainTransferQuantity', { symbol: asset?.symbol ?? '' })}</label><Input id="chain-transfer-amount" value={amount} inputMode="decimal" onChange={v => { invalidate(); setAmount(v); }} maxLength={100}/>
        <Button type="submit" color="primary" disabled={busy || !resolved || !network || !asset || !validAmount || !address.trim()}>{t('reviewPayment')}</Button>
      </form>
    </>}
    {busy && <p role="status">{t('common:loading')}</p>}{error !== undefined && <Failure error={error}/>}
    <Popup visible={!!review} onMaskClick={invalidate} bodyStyle={{ borderTopLeftRadius: 16, borderTopRightRadius: 16 }}>
      {review && network && asset && <div className="payment-review chain-prepare"><h2>{t(walletStep ? 'chainChooseWallet' : 'reviewPayment')}</h2>
        <p>{t(`chainNetwork_${network.id}`)} · {asset.symbol}</p><p className="chain-address">{t('chainRecipientAddress')}: {review.target.address}</p>
        <strong>{review.amount} {asset.symbol}</strong><p>{t('chainSameAsset')}</p><p>{t('chainNoQuote')}</p>
        {!walletStep ? <Button block color="primary" onClick={() => setWalletStep(true)}>{t('chainChooseWallet')}</Button> : <>
          <label htmlFor="chain-wallet-search">{t('nativeSearch')}</label><Input id="chain-wallet-search" value={search} onChange={setSearch}/>
          {compatible.filter(w => `${w.name} ${addressFor(w, network)}`.toLowerCase().includes(search.toLowerCase())).map(w => <Button key={w.id} block aria-pressed={w.id === walletID} onClick={() => setWalletID(w.id)}>{w.name} · {t(w.source === 'local' ? 'chainLocal' : 'chainExternal')}</Button>)}
          {!compatible.length && <p>{t('chainNoCompatibleWallet')}</p>}
          {wallet && <p className="chain-address">{t('chainFromAddress')}: {addressFor(wallet, network)}</p>}
          <p><Link to="/wallet-connections" state={{ from: chainPreparationRoute(review.target.receiveCode) }}>{t('nativeWallets')}</Link></p>
          <p role="status">{t('chainNotOpen')}</p><Button block color="primary" disabled>{t('confirmPayment')}</Button>
        </>}
        <Button block onClick={invalidate}>{t('cancel')}</Button>
      </div>}
    </Popup>
  </>;
}
