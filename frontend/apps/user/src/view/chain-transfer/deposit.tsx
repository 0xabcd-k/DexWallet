import { useEffect, useRef, useState } from 'react';
import { Button, Popup } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { ChainAsset, DepositQuoteData } from '@dexwallet/api-contract/platform';
import { Failure } from '../../auth/context';
import { previewDeposit, validDepositTarget } from '../../app/native/chain-transfer/deposit';
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
        <DepositFields assetID={assetID} setAssetID={setAssetID} target={target} setTarget={setTarget}/>
      </section>}
    </Popup>
  </>;
}

function DepositFields({ assetID, setAssetID, target, setTarget }: {
  assetID: string; setAssetID: (id: string) => void; target: string; setTarget: (v: string) => void;
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
    {native && <DepositEstimate key={selected.asset.id + ':' + target} asset={selected.asset} native={native} target={target}/>}
  </>;
}

export function DepositEstimate({ asset, native, target, preview = previewDeposit }: {
  asset: ChainAsset; native: ChainAsset; target: string; preview?: typeof previewDeposit;
}) {
  const { t } = useTranslation();
  const [quote, setQuote] = useState<DepositQuoteData & { deadline: number }>(), [busy, setBusy] = useState(false);
  const [expired, setExpired] = useState(false), [error, setError] = useState<unknown>();
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
    setQuote(undefined); setError(undefined); setBusy(true);
    try {
      const next = await preview(asset, native, target, request.signal);
      if (!request.signal.aborted) {
        const deadline = started + (next.expiresAt - next.quotedAt);
        setExpired(Date.now() >= deadline);
        setQuote({ ...next, deadline });
      }
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  return <>
    <Button block color="primary" disabled={!validDepositTarget(target) || busy} loading={busy} onClick={() => void estimate()}>{t('depositGetEstimate')}</Button>
    {error !== undefined && <Failure error={error}/>}
    {quote && (expired ? <p role="status">{t('depositExpired')}</p> : <dl aria-live="polite">
      <dt>{t('depositRate')}</dt><dd>1 {asset.symbol} = ${quote.usdPerCoin}</dd>
      <dt>{t('depositPay')}</dt><dd>{quote.payQuantity} {asset.symbol}</dd>
      <dt>{t('depositGas')}</dt><dd>{quote.estimatedGas} {native.symbol} · ${quote.gasUsd}</dd>
      <dt>{t('depositNet')}</dt><dd>${quote.netUsd}</dd>
      <dt>{t('depositValidUntil')}</dt><dd>{new Date(quote.deadline).toLocaleTimeString()}</dd>
    </dl>)}
  </>;
}
