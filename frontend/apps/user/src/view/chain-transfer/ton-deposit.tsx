import { useEffect, useRef, useState } from 'react';
import { Button } from 'antd-mobile';
import { Link } from 'react-router';
import type { DepositQuoteData, TonDepositOrderData } from '@dexwallet/api-contract/platform';
import { useTranslation } from '@dexwallet/i18n';
import { authorizeTonDeposit, getTonOrder, lastTonQuoteKey } from '../../app/native/chain-transfer/ton-deposit';
import { nativeCatalog } from '../../app/native/local-wallet/bridge';
import { Failure } from '../../auth/context';

const states = { awaiting_signature: 'basePending', accepted: 'tonAccepted', signed: 'tonSigned', expired: 'baseExpired', review: 'solDepositReview', credited: 'solDepositCredited' } as const;
export function TonDepositOrder({ idx, local, quote, expired }: {
  idx: string; local?: { userIdx: string; walletId: string }; quote?: DepositQuoteData & { deadline: number }; expired?: boolean;
}) {
  const { t } = useTranslation();
  const [order, setOrder] = useState<TonDepositOrderData>(), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
  const active = useRef<AbortController>();
  useEffect(() => () => active.current?.abort(), []);
  async function run(sign: boolean) {
    const request = new AbortController();
    active.current = request; setBusy(true); setError(undefined);
    try {
      let result;
      if (sign && quote && local) {
        await nativeCatalog.set({ key: lastTonQuoteKey(local.userIdx), value: idx });
        result = await authorizeTonDeposit(local.userIdx, local.walletId, quote, quote.deadline, request.signal);
      } else result = await getTonOrder(idx, request.signal);
      if (!request.signal.aborted) setOrder(result);
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  return <>
    {quote && <dl><dt>{t('tonRelayRecipient')}</dt><dd className="chain-address">{quote.tonRecipient}</dd>
      <dt>{t('tonSigningUntil')}</dt><dd>{new Date(quote.tonValidUntil! * 1000).toLocaleString()}</dd></dl>}
    {quote && local && (!order || order.state === 'awaiting_signature') &&
      <Button color="primary" block disabled={busy || expired} onClick={() => void run(true)}>{t(quote.tonJettonWalletCode ? 'tonSignJettonDeposit' : 'tonSignDeposit')}</Button>}
    <Button disabled={busy} onClick={() => void run(false)}>{t('tonOrderLookup')}</Button>
    {error !== undefined && <Failure error={error}/>}
    {order && <p role="status">{t(states[order.state as keyof typeof states], { idx, amount: order.creditedUsd })}</p>}
    {order?.dispatchBodyHash && <dl><dt>{t('tonDispatchBody')}</dt><dd className="chain-address">{order.dispatchBodyHash}</dd>
      {order.receiveTxHash && <><dt>{t('tonReceiveHash')}</dt><dd className="chain-address">{order.receiveTxHash}</dd></>}
      {order.actualFeeNano && <><dt>{t('tonActualTraceFee')}</dt><dd>{order.actualFeeNano} nanoTON</dd></>}</dl>}
    {order?.jettonExcessNano && <dl>
      <dt>{t('tonActualSenderCost')}</dt><dd>{order.jettonSenderFeeNano} nanoTON</dd>
      <dt>{t('tonRecoverableCost')}</dt><dd>{order.recoverableFeeNano} nanoTON</dd>
      <dt>{t('tonActualExcess')}</dt><dd>{order.jettonExcessNano} nanoTON</dd>
      <dt>{t('tonActualReserve')}</dt><dd>{order.jettonReserveNano} nanoTON</dd>
      <dt>{t('tonActualSurplus')}</dt><dd>{order.relaySurplusNano} nanoTON</dd>
    </dl>}
    {order?.state === 'credited'  && <Link to="/records?kind=usd_balance">{t('viewAccountEntries')}</Link>}
  </>;
}
export function LastTonDeposit({ userIdx }: { userIdx: string }) {
  const [idx, setIdx] = useState<string>();
  useEffect(() => {
    let open = true;
    void nativeCatalog.get({ key: lastTonQuoteKey(userIdx) }).then(({ value }) => {
      if (open && value && /^[a-f0-9]{32}$/.test(value)) setIdx(value);
    }).catch(() => {});
    return () => { open = false; };
  }, [userIdx]);
  return idx ? <TonDepositOrder key={idx} idx={idx}/> : null;
}
