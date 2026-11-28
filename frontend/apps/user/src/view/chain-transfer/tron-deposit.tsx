import { useEffect, useRef, useState } from 'react';
import { Button } from 'antd-mobile';
import { Link } from 'react-router';
import { useTranslation } from '@dexwallet/i18n';
import type { TronDepositOrderData, DepositQuoteData } from '@dexwallet/api-contract/platform';
import { nileTransferLifetime, authorizeTronDeposit, getTronOrder, lastTronQuoteKey } from '../../app/native/chain-transfer/tron-deposit';
import { nileDepositUSDT, nileDepositUSDTContract, nileDepositEnergyLimitSun } from '../../app/native/chain-transfer/tron-deposit-asset';
import { nativeCatalog } from '../../app/native/local-wallet/bridge';
import { Failure } from '../../auth/context';

const labels = { awaiting_signature: 'basePending', signed: 'tronSigned',
  review: 'solDepositReview', expired: 'baseExpired', credited: 'solDepositCredited' } as const;

export function TronDepositOrder({ idx, local, quote, expired }: {
  idx: string; local?: { userIdx: string; walletId: string }; quote?: DepositQuoteData & { deadline: number }; expired?: boolean;
}) {
  const { t } = useTranslation();
  const [order, setOrder] = useState<TronDepositOrderData>(), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
  const active = useRef<AbortController>();
  useEffect(() => () => active.current?.abort(), []);
  async function run(sign: boolean) {
    const request = new AbortController();
    active.current = request; setBusy(true); setError(undefined);
    try {
      let result;
      if (sign && quote && local) {
        await nativeCatalog.set({ key: lastTronQuoteKey(local.userIdx), value: idx });
        result = await authorizeTronDeposit(local.userIdx, local.walletId, quote, quote.deadline, request.signal);
      } else result = await getTronOrder(idx, request.signal);
      if (quote && (result.assetId !== quote.assetId || (result.txId && result.txId !== quote.messageSha256))) throw new TypeError('Unexpected Nile order transaction');
      if (!request.signal.aborted) setOrder(result);
    } catch (e) { if (!request.signal.aborted) setError(e); }
    finally { if (!request.signal.aborted) setBusy(false); }
  }
  const token = (quote?.assetId ?? order?.assetId) === nileDepositUSDT;
  const asset = token ? t('tronFixtureUSDT') : 'TRX';
  return <>
    {quote && <dl><dt>{t('tronRecipient', { asset })}</dt><dd className="chain-address">{quote.tronRecipient}</dd>
      <dt>{t('tronResourceOwner')}</dt><dd className="chain-address">{quote.tronResourceOwner}</dd>
      <dt>{t('tronExpiry')}</dt><dd>{new Date(quote.tronRefTime! + nileTransferLifetime).toLocaleString()}</dd></dl>}
    {quote && token && <dl><dt>{t('tronTokenContract')}</dt><dd className="chain-address">{nileDepositUSDTContract}</dd>
      <dt>{t('tronEnergyLimit')}</dt><dd>{nileDepositEnergyLimitSun / 1_000_000} TRX</dd></dl>}
    {quote && <p>{t(token ? 'tronTokenResourceNotice' : 'tronResourceNotice')}</p>}
    {local && (!order || order.state === 'awaiting_signature') && <Button color="primary" block disabled={busy || expired} onClick={() => void run(true)}>{t('tronSign', { asset })}</Button>}
    <Button disabled={busy} onClick={() => void run(false)}>{t(quote || order ? 'tronLookupAsset' : 'tronLookup', { asset })}</Button>
    {error !== undefined && <Failure error={error}/>}
    {order && <p role="status">{t(labels[order.state as keyof typeof labels], { idx, amount: order.creditedUsd })}</p>}
    {order?.txId && <dl><dt>{t('tronDepositAsset')}</dt><dd>{asset}</dd><dt>{t('tronTx')}</dt><dd className="chain-address">{order.txId}</dd>
      {order.actualBandwidth !== undefined && <><dt>{t('tronActualBandwidth')}</dt><dd>{order.actualBandwidth}</dd></>}
      {token && order.actualFeeSun !== undefined && <><dt>{t('tronActualEnergy')}</dt><dd>{order.actualEnergy ?? 0}</dd></>}
      {order.actualFeeSun !== undefined && <><dt>{t('tronActualFee')}</dt><dd>{order.actualFeeSun} sun</dd></>}</dl>}
    {order?.state === 'credited' && <Link to="/records?kind=usd_balance">{t('viewAccountEntries')}</Link>}
  </>;
}
export function LastTronDeposit({ userIdx }: { userIdx: string }) {
  const [idx, setIdx] = useState<string>();
  useEffect(() => {
    let open = true;
    void nativeCatalog.get({ key: lastTronQuoteKey(userIdx) }).then(({ value }) => {
      if (open && value && /^[a-f0-9]{32}$/.test(value)) setIdx(value);
    }).catch(() => {});
    return () => { open = false; };
  }, [userIdx]);
  return idx ? <TronDepositOrder key={idx} idx={idx}/> : null;
}
