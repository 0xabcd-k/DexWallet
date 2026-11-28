import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { Button } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { BeneficiaryPaymentsData, DepositQuoteData, SolDepositOrderData } from '@dexwallet/api-contract/platform';
import { Failure } from '../../auth/context';
import { nativeCatalog } from '../../app/native/local-wallet/bridge';
import { authorizeSolDeposit, getBeneficiaryPayments, getSolOrder, lastSolQuoteKey } from '../../app/native/chain-transfer/sol-deposit';

function OrderStatus({ order }: { order: SolDepositOrderData }) {
  const { t } = useTranslation();
  const statuses = { waiting: 'solDepositAccepted', signed: 'solDepositSigned', submitted: 'solDepositSubmitted',
    review: 'solDepositReview', finalized: 'solDepositFinalized', failed: 'solDepositFailed' } as const;
  const label = order.creditedUsd !== undefined ? (order.beneficiaryIdx ? 'beneficiaryCredited' : 'solDepositCredited') : order.state === 'accepted' ? statuses[order.executionState] :
    Date.now() >= order.quoteExpiresAt ? 'solDepositLate' : 'solDepositPending';
  return <><p role="status">{t(label, { idx: order.orderIdx, amount: order.creditedUsd, beneficiary: order.beneficiaryIdx })}</p>
    {order.beneficiaryIdx && !order.creditedUsd && <p>{t('recipientIdx')} · {order.beneficiaryIdx}</p>}
    {order.creditedUsd !== undefined && !order.beneficiaryIdx && <Link to="/records?kind=usd_balance">{t('viewAccountEntries')}</Link>}
    {order.txSignature && <dl><dt>{t('solDepositTransaction')}</dt><dd className="chain-address">{order.txSignature}</dd>
      {order.actualFeeLamports !== undefined && <><dt>{t('solDepositActualFee')}</dt><dd>{order.actualFeeLamports} lamports</dd></>}
      {order.actualFundingLamports !== undefined && <><dt>{t('solDepositFunding')}</dt><dd>{order.actualFundingLamports} lamports</dd></>}</dl>}</>;
}

function Lookup({ idx, initial }: { idx: string; initial?: SolDepositOrderData }) {
  const { t } = useTranslation();
  const [order, setOrder] = useState(initial), [error, setError] = useState<unknown>(), [busy, setBusy] = useState(false);
  const active = useRef<AbortController>();
  useEffect(() => () => active.current?.abort(), []);
  async function lookup() {
    const request = new AbortController();
    active.current = request; setBusy(true); setError(undefined);
    try {
      const o = await getSolOrder(idx, request.signal);
      if (!request.signal.aborted) setOrder(o);
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  return <><Button disabled={busy} onClick={() => void lookup()}>{t('solDepositLookup')}</Button>
    {error !== undefined && <Failure error={error}/>}
    {order && <OrderStatus order={order}/>}</>;
}

export function LastSolDeposit({ userIdx, payments }: { userIdx: string; payments?: BeneficiaryPaymentsData['items'] }) {
  const [idx, setIdx] = useState<string>();
  const forOther = payments !== undefined;
  useEffect(() => {
    let active = true;
    void nativeCatalog.get({ key: lastSolQuoteKey(userIdx, forOther) }).then(({ value }) => {
      if (active && value && /^[0-9a-f]{32}$/.test(value)) setIdx(value);
    }).catch(() => {});
    return () => { active = false; };
  }, [userIdx, forOther]);
  return idx && !payments?.some(p => p.quoteIdx === idx) ? <Lookup key={idx} idx={idx}/> : null;
}

export function SolDepositAuthorization({ local, quote, expired }: {
  local: { userIdx: string; walletId: string }; quote: DepositQuoteData & { deadline: number }; expired: boolean;
}) {
  const { t } = useTranslation();
  const [order, setOrder] = useState<SolDepositOrderData>(), [error, setError] = useState<unknown>(), [busy, setBusy] = useState(false);
  const active = useRef<AbortController>();
  useEffect(() => () => active.current?.abort(), []);
  async function sign() {
    if (busy || expired || !quote.quoteIdx) return;
    const request = new AbortController();
    active.current = request; setBusy(true); setError(undefined);
    try {
      await nativeCatalog.set({ key: lastSolQuoteKey(local.userIdx, !!quote.beneficiaryIdx), value: quote.quoteIdx });
      request.signal.throwIfAborted();
      const accepted = await authorizeSolDeposit(local.userIdx, local.walletId, quote, quote.deadline, request.signal);
      if (!request.signal.aborted) setOrder(accepted);
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  if (order) return <Lookup idx={order.quoteIdx} initial={order}/>;
  return <><dl><dt>{t('solDepositTarget')}</dt><dd className="chain-address">{quote.solRecipient}</dd>
    <dt>{t('solDepositSponsor')}</dt><dd className="chain-address">{quote.solFeePayer}</dd></dl>
    <Button color="primary" block disabled={busy || expired} onClick={() => void sign()}>{t('solDepositSign')}</Button>
    {error !== undefined && <><Failure error={error}/><Lookup idx={quote.quoteIdx!}/></>}</>;
}

export function BeneficiaryPaymentHistory({ userIdx }: { userIdx: string }) {
  const { t } = useTranslation();
  const [items, setItems] = useState<BeneficiaryPaymentsData['items']>(), [next, setNext] = useState('');
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
  const active = useRef<AbortController>();
  useEffect(() => () => active.current?.abort(), []);
  async function load(before = '') {
    active.current?.abort();
    const request = new AbortController(); active.current = request;
    setBusy(true); setError(undefined);
    try {
      const data = await getBeneficiaryPayments(before, request.signal);
      if (!request.signal.aborted) {
        setItems(prev => before ? [...(prev ?? []), ...data.items] : data.items);
        setNext(data.nextCursor ?? '');
      }
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  return <><Button block disabled={busy} onClick={() => void load()}>{t('beneficiaryPayments')}</Button>
    {error !== undefined && <Failure error={error}/>}
    {items?.length === 0 && <p role="status">{t('beneficiaryEmpty')}</p>}
    {items && <LastSolDeposit userIdx={userIdx} payments={items}/>}
    {items?.map(p => <article key={p.quoteIdx}><p>{t('recipientIdx')} · {p.beneficiaryIdx}</p>
      <p>{p.payQuantity} SOL · {t('depositTarget')} ${p.targetUsd} · {new Date(p.acceptedAt).toLocaleString()}</p>
      <Lookup idx={p.quoteIdx}/></article>)}
    {next && <Button disabled={busy} onClick={() => void load(next)}>{t('loadMoreRecords')}</Button>}</>;
}
