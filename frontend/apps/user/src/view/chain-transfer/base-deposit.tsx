import { useEffect, useRef, useState } from 'react';
import { Button } from 'antd-mobile';
import { Link } from 'react-router';
import { useTranslation } from '@dexwallet/i18n';
import type { BaseDepositOrderData, DepositQuoteData } from '@dexwallet/api-contract/platform';
import { authorizeBaseDeposit, getBaseOrder, lastBaseQuoteKey } from '../../app/native/chain-transfer/base-deposit';
import { nativeCatalog } from '../../app/native/local-wallet/bridge';
import { Failure } from '../../auth/context';

const labels = { awaiting_signature: 'basePending', accepted: 'baseAccepted', signed: 'baseSigned',
  review: 'solDepositReview', expired: 'baseExpired', credited: 'solDepositCredited' } as const;

export function BaseDepositOrder({ idx, local, quote, expired }: {
  idx: string; local?: { userIdx: string; walletId: string }; quote?: DepositQuoteData & { deadline: number }; expired?: boolean;
}) {
  const { t } = useTranslation();
  const [order, setOrder] = useState<BaseDepositOrderData>(), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
  const active = useRef<AbortController>();
  const asset = (quote?.assetId ?? order?.assetId) === 'base-sepolia:eth' ? 'ETH' : 'USDC';
  useEffect(() => () => active.current?.abort(), []);
  async function run(sign: boolean) {
    const request = new AbortController();
    active.current = request; setBusy(true); setError(undefined);
    try {
      let result;
      if (sign && quote && local) {
        await nativeCatalog.set({ key: lastBaseQuoteKey(local.userIdx), value: idx });
        result = await authorizeBaseDeposit(local.userIdx, local.walletId, quote, quote.deadline, request.signal);
      } else result = await getBaseOrder(idx, request.signal);
      if (!request.signal.aborted) setOrder(result);
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  return <>
    {quote && <dl><dt>{t('baseRecipient', { asset })}</dt><dd className="chain-address">{quote.baseRecipient}</dd>
      <dt>{t('baseSponsor')}</dt><dd className="chain-address">{quote.baseFeePayer}</dd>
      <dt>{t('baseValidBefore', { asset })}</dt><dd>{new Date(quote.baseValidBefore! * 1000).toLocaleString()}</dd></dl>}
    {local && (!order || order.state === 'awaiting_signature') && <Button color="primary" block disabled={busy || expired} onClick={() => void run(true)}>{t('baseSign', { asset })}</Button>}
    <Button disabled={busy} onClick={() => void run(false)}>{t('baseLookup')}</Button>
    {error !== undefined && <Failure error={error}/>}
    {order && <p role="status">{t(labels[order.state as keyof typeof labels], { idx, amount: order.creditedUsd })}</p>}
    {order?.txHash && <dl><dt>{t('chainToken')}</dt><dd>{asset}</dd><dt>{t('baseTx')}</dt><dd className="chain-address">{order.txHash}</dd>
      {order.actualFeeWei !== undefined && <><dt>{t('solDepositActualFee')}</dt><dd>{order.actualFeeWei} wei</dd></>}</dl>}
    {order?.state === 'credited' && <Link to="/records?kind=usd_balance">{t('viewAccountEntries')}</Link>}
  </>;
}
export function LastBaseDeposit({ userIdx }: { userIdx: string }) {
  const [idx, setIdx] = useState<string>();
  useEffect(() => {
    let open = true;
    void nativeCatalog.get({ key: lastBaseQuoteKey(userIdx) }).then(({ value }) => {
      if (open && value && /^[a-f0-9]{32}$/.test(value)) setIdx(value);
    }).catch(() => {});
    return () => { open = false; };
  }, [userIdx]);
  return idx ? <BaseDepositOrder key={idx} idx={idx}/> : null;
}
