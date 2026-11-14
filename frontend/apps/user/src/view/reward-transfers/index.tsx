import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { Button, Dialog, Input } from 'antd-mobile';
import { formatTime, useTranslation } from '@dexwallet/i18n';
import type { RewardTransferStateData, RewardTransferRequest, RewardTransferData, RewardTransferListData } from '@dexwallet/api-contract/platform';
import { getRewardTransferState, submitRewardTransfer, rewardTransferByRequest, getRewardTransfer, getRewardTransfers, usdCents, MinimumRewardCents } from '../../api/reward-transfers';
import { Failure, useAuth } from '../../auth/context';
import { readRewardTransfer, prepareRewardTransfer, clearRewardTransfer } from './pending';
import './style.less';
function Result({ data }: { data: RewardTransferData }) {
  const { t, i18n } = useTranslation();
  return <article className="reward-transfer-result" aria-live="polite"><h2>{t(`rt_${data.status}`)}</h2>
    <p><strong>${data.amount} USD</strong> · {t('rtFee')}</p><p>{t(`rtExplain_${data.status}`)}</p>
    {data.reviewReason && <p>{t('rtReviewReason')}: {data.reviewReason}</p>}
    <p>{formatTime(Date.parse(data.createdAt), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en')}</p>
    <Link to={`/reward-transfers/${data.idx}`}>{t('rtDetail')}</Link> · <Link to="/records">{t('records')}</Link>
  </article>;
}
export default function RewardTransfers() { const auth = useAuth(); return <TransferPage key={auth.session.generation} />; }
function TransferPage() {
  const { t } = useTranslation(); const auth = useAuth(); const userIdx = auth.session.credential!.idx;
  const [state, setState] = useState<RewardTransferStateData>(); const [revision, setRevision] = useState(0); const [loadError, setLoadError] = useState<unknown>();
  const [amount, setAmount] = useState(''); const [error, setError] = useState<unknown>(); const [pending, setPending] = useState<RewardTransferRequest>();
  const [result, setResult] = useState<RewardTransferData>(); const [busy, setBusy] = useState(false);
  const operation = useRef<AbortController | undefined>(undefined);
  useEffect(() => { const c = new AbortController(); setLoadError(undefined);
    void getRewardTransferState(c.signal).then(d => { if (!c.signal.aborted) setState(d); }, e => { if (!c.signal.aborted) setLoadError(e); }); return () => c.abort();
  }, [revision]);
  useEffect(() => { try { setPending(readRewardTransfer(localStorage, userIdx)); } catch (e) { setError(e); }
    return () => operation.current?.abort(); }, [userIdx]);
  async function accept(data: RewardTransferData, req: RewardTransferRequest, c: AbortController) {
    if (data.userIdx !== userIdx || usdCents(data.amount) !== usdCents(req.amount)) throw new Error('Mismatched reward transfer');
    if (c.signal.aborted) return; await clearRewardTransfer(userIdx, req.requestKey);
    if (!c.signal.aborted) { setPending(undefined); setResult(data); setAmount(''); setError(undefined); setRevision(n => n + 1); }
  }
  async function execute(req: RewardTransferRequest, lookup: boolean, c: AbortController) {
    try { await accept(await (lookup ? rewardTransferByRequest(req.requestKey, c.signal) : submitRewardTransfer(req, c.signal)), req, c); }
    catch (e) {
      if (c.signal.aborted) return; setError(e);
      // Even a refusal and a lookup miss cannot terminate an older in-flight
      // risk evaluation. Keep the original key and amount for explicit retry.
    }
  }
  async function run(work: (c: AbortController) => Promise<void>) {
    if (operation.current && !operation.current.signal.aborted) return;
    const c = new AbortController(); operation.current = c; setBusy(true); setError(undefined);
    try { await work(c); } catch (e) { if (!c.signal.aborted) setError(e); }
    finally { if (!c.signal.aborted) setBusy(false); c.abort(); }
  }
  const cents = usdCents(amount); const balance = state ? usdCents(state.rewardUsd) : undefined;
  const valid = cents !== undefined && cents >= MinimumRewardCents && balance !== undefined && cents <= balance;
  return <section className="reward-transfer-page"><Link to="/rewards">{t('rewards')}</Link><h1>{t('rtTitle')}</h1>
    <p>{t('rtDestination')}</p><p>{t('rtFee')}</p>
    {state && <><dl><dt>{t('rewardCash')}</dt><dd>${state.rewardUsd}</dd><dt>{t('rtInFlight')}</dt><dd>${state.pendingUsd}</dd></dl>
      <p>{t('rtMinimum', { amount: state.minimumAmount })}</p>{!state.pro && <p>{t('rtProRequired')}</p>}{!state.available && <p>{t('rtUnavailable')}</p>}</>}
    {pending ? <div role="status"><p>{t('rtPendingRequest', { amount: pending.amount })}</p>
      <Button disabled={busy} onClick={() => { void run(c => execute(pending, true, c)); }}>{t('rtLookup')}</Button>
      <Button disabled={busy} onClick={() => { void run(c => execute(pending, false, c)); }}>{t('rtRetry')}</Button></div> : <>
      <label>{t('rtAmount')}<Input aria-label={t('rtAmount')} inputMode="decimal" value={amount} onChange={setAmount} disabled={busy} maxLength={21} /></label>
      <Button size="small" disabled={busy || !state} onClick={() => setAmount(state!.rewardUsd)}>{t('rtAll')}</Button>
      {cents !== undefined && balance !== undefined && cents > balance && <p role="alert">{t('rtInsufficient')}</p>}
      <Button color="primary" disabled={busy || !state?.available || !state.pro || !valid || loadError !== undefined} onClick={() => { void run(async c => {
        if (!valid || cents === undefined) return;
        const normalized = `${cents / 100n}.${String(cents % 100n).padStart(2,'0')}`;
        const yes = await Dialog.confirm({ title: t('rtConfirm'), content: t('rtConfirmBody', { amount: normalized }), confirmText: t('rtSubmit'), cancelText: t('common:cancel') });
        if (!yes || c.signal.aborted) return;
        const saved = await prepareRewardTransfer(userIdx, normalized); if (c.signal.aborted) return; setPending(saved.request);
        if (!saved.existing) { setResult(undefined); await execute(saved.request, false, c); }
      }); }}>{t('rtSubmit')}</Button>
    </>}
    {error !== undefined && <Failure error={error} />}{loadError !== undefined && <Failure error={loadError} />}
    {result && <Result data={result} />}
    <p><Link to="/reward-transfer-history">{t('rtHistory')}</Link> · <Button size="small" disabled={busy} onClick={() => setRevision(n => n + 1)}>{t('refreshBalances')}</Button></p>
  </section>;
}
export function RewardTransferHistory() { const a = useAuth(); return <History key={a.session.generation} />; }
function History() {
  const { t } = useTranslation(); const [before, setBefore] = useState(''); const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ data?: RewardTransferListData; error?: unknown }>({});
  useEffect(() => { const c = new AbortController(); setState({}); void getRewardTransfers(before, c.signal).then(data => { if (!c.signal.aborted) setState({ data }); }, error => { if (!c.signal.aborted) setState({ error }); }); return () => c.abort(); }, [before, revision]);
  return <section className="reward-transfer-page"><Link to="/reward-transfers">{t('rtTitle')}</Link><h1>{t('rtHistory')}</h1>
    {state.data?.items.map(d => <Result key={d.idx} data={d} />)}{state.data?.items.length === 0 && <p>{t('rtEmpty')}</p>}
    {state.error !== undefined && <Failure error={state.error} />}<Button onClick={() => setRevision(n => n + 1)}>{t('refreshRecords')}</Button>
    {state.data?.nextCursor && <Button onClick={() => setBefore(state.data!.nextCursor!)}>{t('taskNext')}</Button>}{before && <Button onClick={() => setBefore('')}>{t('taskFirst')}</Button>}
  </section>;
}
export function RewardTransferDetail() { const { idx = '' } = useParams(); const a = useAuth(); return <Detail key={`${a.session.generation}:${idx}`} idx={idx} />; }
function Detail({ idx }: { idx: string }) {
  const { t } = useTranslation(); const [data, setData] = useState<RewardTransferData>(); const [error, setError] = useState<unknown>(); const [revision, setRevision] = useState(0);
  useEffect(() => { const c = new AbortController(); setError(undefined); void getRewardTransfer(idx, c.signal).then(d => { if (!c.signal.aborted) setData(d); }, e => { if (!c.signal.aborted) setError(e); }); return () => c.abort(); }, [idx, revision]);
  return <section className="reward-transfer-page"><Link to="/reward-transfer-history">{t('rtHistory')}</Link>{data && <Result data={data} />}
    {error !== undefined && <Failure error={error} />}<Button onClick={() => setRevision(n => n + 1)}>{t('refreshRecords')}</Button>
  </section>;
}
