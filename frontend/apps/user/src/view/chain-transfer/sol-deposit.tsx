import { useEffect, useRef, useState } from 'react';
import { Button } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { DepositQuoteData, SolDepositOrderData } from '@dexwallet/api-contract/platform';
import { Failure } from '../../auth/context';
import { nativeCatalog } from '../../app/native/local-wallet/bridge';
import { authorizeSolDeposit, getSolOrder, lastSolQuoteKey } from '../../app/native/chain-transfer/sol-deposit';

function OrderStatus({ order }: { order: SolDepositOrderData }) {
  const { t } = useTranslation();
  return <p role="status">{t(order.state === 'accepted' ? 'solDepositAccepted' :
    Date.now() >= order.quoteExpiresAt ? 'solDepositLate' : 'solDepositPending', { idx: order.orderIdx })}</p>;
}

function Lookup({ idx }: { idx: string }) {
  const { t } = useTranslation();
  const [order, setOrder] = useState<SolDepositOrderData>(), [error, setError] = useState<unknown>(), [busy, setBusy] = useState(false);
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

export function LastSolDeposit({ userIdx }: { userIdx: string }) {
  const [idx, setIdx] = useState<string>();
  useEffect(() => {
    let active = true;
    void nativeCatalog.get({ key: lastSolQuoteKey(userIdx) }).then(({ value }) => {
      if (active && value && /^[0-9a-f]{32}$/.test(value)) setIdx(value);
    }).catch(() => {});
    return () => { active = false; };
  }, [userIdx]);
  return idx ? <Lookup key={idx} idx={idx}/> : null;
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
      await nativeCatalog.set({ key: lastSolQuoteKey(local.userIdx), value: quote.quoteIdx });
      request.signal.throwIfAborted();
      const accepted = await authorizeSolDeposit(local.userIdx, local.walletId, quote, quote.deadline, request.signal);
      if (!request.signal.aborted) setOrder(accepted);
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  if (order) return <OrderStatus order={order}/>;
  return <><dl><dt>{t('solDepositTarget')}</dt><dd className="chain-address">{quote.solRecipient}</dd>
    <dt>{t('solDepositSponsor')}</dt><dd className="chain-address">{quote.solFeePayer}</dd></dl>
    <Button color="primary" block disabled={busy || expired} onClick={() => void sign()}>{t('solDepositSign')}</Button>
    {error !== undefined && <><Failure error={error}/><Lookup idx={quote.quoteIdx!}/></>}</>;
}
