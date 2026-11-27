import { useEffect, useRef, useState } from 'react';
import { Button, Popup } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { ChainAsset, ChainNetwork, DepositQuoteData } from '@dexwallet/api-contract/platform';
import { Failure } from '../../auth/context';
import { confirmDepositQuote, previewDeposit, validDepositTarget } from '../../app/native/chain-transfer/deposit';
import { addressFor } from '../../app/native/chain-assets/wallets';
import { useViewingWallets } from '../../app/native/chain-assets/use-wallets';
import { Link } from 'react-router';
import { useChainCatalog } from './sources';

export default function DepositButton({ userIdx }: { userIdx: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false), [assetID, setAssetID] = useState(''), [target, setTarget] = useState('');
  return <>
    <Button block onClick={() => setOpen(true)}>{t('deposit')}</Button>
    <Popup visible={open} onMaskClick={() => setOpen(false)} onClose={() => setOpen(false)} showCloseButton>
      {open && <section className="payment-review transfer-result deposit-quote" role="dialog" aria-label={t('depositQuoteTitle')}>
        <h2>{t('depositQuoteTitle')}</h2><p>{t('depositBeneficiary', { idx: userIdx })}</p>
        <p>{t('depositTestNote')}</p>
        <DepositFields key={userIdx} userIdx={userIdx} assetID={assetID} setAssetID={setAssetID} target={target} setTarget={setTarget}/>
      </section>}
    </Popup>
  </>;
}

function DepositFields({ userIdx, assetID, setAssetID, target, setTarget }: {
  userIdx: string; assetID: string; setAssetID: (id: string) => void; target: string; setTarget: (v: string) => void;
}) {
  const { t } = useTranslation(), catalog = useChainCatalog();
  if (catalog.loading) return <p role="status">{t('common:loading')}</p>;
  if (catalog.error !== undefined) return <><Failure error={catalog.error}/><Button onClick={catalog.retry}>{t('common:retry')}</Button></>;
  const choices = catalog.networks.filter(n => n.testnet).flatMap(network => network.assets
    .filter(a => a.depositPreview === true).map(asset => ({ network, asset })));
  if (!choices.length) return <><p>{t('depositNoPrices')}</p><Button onClick={catalog.retry}>{t('common:retry')}</Button></>;
  const selected = choices.find(c => c.asset.id === assetID) ?? choices[0]!;
  const native = selected.network.assets.find(a => a.origin === 'native');
  return <>
    <label>{t('chainToken')}<select value={selected.asset.id} onChange={e => setAssetID(e.target.value)}>
      {choices.map(({ network, asset }) => <option key={asset.id} value={asset.id}>{t(`chainNetwork_${network.id}`)} · {asset.symbol}{asset.origin === 'test_fixture' ? ` · ${t('chainFixture')}` : ''}</option>)}
    </select></label>
    <label>{t('depositTarget')}<input value={target} inputMode="decimal" maxLength={21} onChange={e => setTarget(e.target.value)}/></label>
    {native && <DepositSteps key={selected.asset.id + ':' + target} userIdx={userIdx} network={selected.network} asset={selected.asset} native={native} target={target}/>}
  </>;
}

type EstimateInput = { asset: ChainAsset; native: ChainAsset; target: string };

function DepositSteps({ userIdx, network, ...input }: EstimateInput & { userIdx: string; network: ChainNetwork }) {
  const { t } = useTranslation();
  const [chooseWallet, setChooseWallet] = useState(false);
  if (!chooseWallet) return <DepositEstimate {...input} onChooseWallet={() => setChooseWallet(true)}/>;
  return <>
    <WalletEstimate {...input} userIdx={userIdx} network={network}/>
    <Button block onClick={() => setChooseWallet(false)}>{t('cancel')}</Button>
  </>;
}

function WalletEstimate({ userIdx, network, ...input }: EstimateInput & { userIdx: string; network: ChainNetwork }) {
  const { t } = useTranslation(), source = useViewingWallets(userIdx);
  const [walletID, setWalletID] = useState(''), [search, setSearch] = useState('');
  const compatible = source.wallets.filter(w => addressFor(w, network));
  const wallet = compatible.find(w => w.id === walletID);
  const address = wallet && addressFor(wallet, network);
  return <>
    {input.native.id === 'ton-testnet:ton' && <p>{t('depositTONRelay')}</p>}
    {input.asset.id === 'base-sepolia:eth' && <p>{t('depositETHDelegation')}</p>}
    <label>{t('nativeSearch')}<input type="search" value={search} onChange={e => setSearch(e.target.value)}/></label>
    {source.error !== undefined && <><Failure error={source.error}/><Button onClick={source.refresh}>{t('common:retry')}</Button></>}
    {source.loading && <p role="status">{t('common:loading')}</p>}
    {!source.loading && !compatible.length && <p>{t('chainNoCompatibleWallet')} <Link to="/local-wallets">{t('localWallets')}</Link></p>}
    {compatible.filter(w => `${w.name} ${addressFor(w, network)}`.toLowerCase().includes(search.toLowerCase())).map(w =>
      <Button key={w.id} block aria-pressed={w.id === walletID} onClick={() => setWalletID(w.id)}>{w.name} · {t(w.source === 'local' ? 'chainLocal' : 'chainExternal')}</Button>)}
    {address && <><p className="chain-address">{t('chainFromAddress')}: {address}</p>
      <DepositEstimate key={address} {...input} walletAddress={address}/></>}
  </>;
}

export function DepositEstimate({ asset, native, target, walletAddress, onChooseWallet, preview = previewDeposit }: EstimateInput & {
  walletAddress?: string; onChooseWallet?: () => void; preview?: typeof previewDeposit;
}) {
  const { t } = useTranslation();
  const [quote, setQuote] = useState<DepositQuoteData & { deadline: number }>(), [busy, setBusy] = useState(false);
  const [expired, setExpired] = useState(false), [error, setError] = useState<unknown>();
  const [confirmedAt, setConfirmedAt] = useState<number>(), [attempted, setAttempted] = useState(false);
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => active.current?.abort(), []);
  useEffect(() => {
    if (!quote) return;
    const until = quote.deadline;
    const check = () => setExpired(Date.now() >= until);
    check();
    const timer = window.setTimeout(check, Math.max(0, until - Date.now()));
    document.addEventListener('visibilitychange', check);
    return () => { clearTimeout(timer); document.removeEventListener('visibilitychange', check); };
  }, [quote]);
  async function estimate() {
    active.current?.abort();
    const started = Date.now();
    const request = new AbortController(); active.current = request;
    setQuote(undefined); setConfirmedAt(undefined); setAttempted(false); setError(undefined); setBusy(true);
    try {
      const next = await preview(asset, native, target, request.signal, walletAddress);
      if (!request.signal.aborted) {
        const deadline = started + (next.expiresAt - next.quotedAt);
        setExpired(Date.now() >= deadline);
        setQuote({ ...next, deadline });
      }
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  async function confirm() {
    if (!quote?.quoteIdx || busy || confirmedAt !== undefined) return;
    const { deadline, ...terms } = quote;
    const lookupOnly = Date.now() >= deadline;
    if (lookupOnly && !attempted) { setExpired(true); return; }
    const request = new AbortController(); active.current = request;
    setError(undefined); setBusy(true); setAttempted(true);
    try {
      const at = await confirmDepositQuote(terms, lookupOnly, request.signal);
      if (!request.signal.aborted) setConfirmedAt(at);
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  return <>
    <Button block color="primary" disabled={!validDepositTarget(target) || busy} loading={busy} onClick={() => void estimate()}>{t(walletAddress ? 'depositGetWalletFee' : 'depositGetEstimate')}</Button>
    {error !== undefined && <Failure error={error}/>}
    {quote && (expired && confirmedAt === undefined ? <p role="status">{t('depositExpired')}</p> : <dl aria-live="polite">
      <dt>{t('depositRate')}</dt><dd>1 {asset.symbol} = ${quote.usdPerCoin}</dd>
      <dt>{t('depositRateSource')}</dt><dd>{t(quote.rateSource === 'kraken' ? 'depositSourceKraken' : 'depositSourceTest')} · {new Date(quote.asOf).toLocaleString()}</dd>
      <dt>{t('depositPay')}</dt><dd>{quote.payQuantity} {asset.symbol}</dd>
      {quote.delegatedEnergy !== undefined && <><dt>{t('depositPlatformEnergy')}</dt><dd>{quote.delegatedEnergy}</dd></>}
      {quote.delegatedBandwidth !== undefined && <><dt>{t('depositPlatformBandwidth')}</dt><dd>{quote.delegatedBandwidth}</dd></>}
      <dt>{t(quote.delegatedBandwidth !== undefined ? 'depositTrxBurn' : quote.feeBlockHash || quote.emulatedMessageHash ? 'depositPreSignGas' : walletAddress ? 'depositNetworkGas' : 'depositGas')}</dt><dd>{quote.estimatedGas} {native.symbol} · ${quote.gasUsd}</dd>
      {quote.maxGas !== undefined && <><dt>{t('depositMaxGas')}</dt><dd>{quote.maxGas} {native.symbol}</dd></>}
      {quote.relayAttached && <><dt>{t('depositRelayAttached')}</dt><dd>{quote.relayAttached} {native.symbol}</dd>
        <dt>{t('depositRelaySurplus')}</dt><dd>{quote.relaySurplus} {native.symbol}</dd></>}
      {quote.jettonExcess && <><dt>{t('depositJettonExcess')}</dt><dd>{quote.jettonExcess} {native.symbol}</dd></>}
      {quote.accountFunding && <><dt>{t(quote.jettonExcess ? 'depositJettonReserve' : 'depositAccountFunding')}</dt><dd>{quote.accountFunding} {native.symbol} · ${quote.accountFundingUsd}</dd></>}
      <dt>{t('depositNet')}</dt><dd>${quote.netUsd}</dd>
      <dt>{t('depositValidUntil')}</dt><dd>{new Date(quote.deadline).toLocaleTimeString()}</dd>
    </dl>)}
    {quote?.quoteIdx && (confirmedAt !== undefined
      ? <p role="status">{t('depositQuoteSaved', { idx: quote.quoteIdx })}</p>
      : (!expired || attempted) && <Button block disabled={busy} onClick={() => void confirm()}>
        {t(expired ? 'depositCheckConfirmation' : 'depositConfirmQuote')}</Button>)}
    {quote?.walletFeePreview === true && !expired && onChooseWallet && <Button block onClick={onChooseWallet}>{t('chainChooseWallet')}</Button>}
  </>;
}
