import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { Button } from 'antd-mobile';
import { ApiError } from '@dexwallet/api-client';
import { formatTime, useTranslation } from '@dexwallet/i18n';
import type { DrawStateData, DrawRequest, DrawResultData, DrawListData } from '@dexwallet/api-contract/platform';
import { getDrawState, createDraw, drawByRequest, getDraw, getDraws } from '../../api/draws';
import { Failure, useAuth } from '../../auth/context';
import { readDraw, prepareDraw, clearDraw } from './pending';
import './style.less';

const symbols = ['','🍒','🍋','🍇','🍉','🍊','🍎','⭐'];
const reviewCodes = new Set(['DRAW_VERSION_CHANGED', 'INSUFFICIENT_BALANCE']);
function Symbols({ values, animate = false }: { values: number[]; animate?: boolean }) {
  return <div className={`draw-symbols${animate ? ' draw-reveal' : ''}`} aria-hidden="true">{values.map((value, i) => <span key={i}>{symbols[value]}</span>)}</div>;
}
function DrawResult({ data, animate = false }: { data: DrawResultData; animate?: boolean }) {
  const { t, i18n } = useTranslation();
  return <article className="draw-result" aria-live="polite"><h2>{t('drawSettled')}</h2>
    <Symbols key={data.idx} values={data.symbols} animate={animate} />
    <p><strong>{t(`drawPrize_${data.prizeCode}`)} · ${data.rewardUsd}</strong></p>
    <p>{t('drawCredited', { points: data.costPoints })}</p>
    <p>{formatTime(Date.parse(data.createdAt), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en')} · {data.versionName}</p>
    <Link to={`/draws/${data.idx}`}>{t('drawRecord')}</Link> · <Link to="/records?kind=reward_usd">{t('records')}</Link>
  </article>;
}
export default function Draws() { const auth = useAuth(); return <DrawPage key={auth.session.generation} />; }
function DrawPage() {
  const { t } = useTranslation(); const auth = useAuth(); const userIdx = auth.session.credential!.idx;
  const [state, setState] = useState<DrawStateData>(); const [revision, setRevision] = useState(0); const [loadError, setLoadError] = useState<unknown>();
  const [error, setError] = useState<unknown>(); const [pending, setPending] = useState<DrawRequest>();
  const [result, setResult] = useState<DrawResultData>(); const [animate, setAnimate] = useState(false); const [busy, setBusy] = useState(false);
  const operation = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    const controller = new AbortController(); setLoadError(undefined);
    void getDrawState(controller.signal).then(data => { if (!controller.signal.aborted) setState(data); }, e => { if (!controller.signal.aborted) setLoadError(e); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => {
    try { setPending(readDraw(localStorage, userIdx)); } catch (e) { setError(e); }
    return () => operation.current?.abort();
  }, [userIdx]);
  async function accept(data: DrawResultData, request: DrawRequest, controller: AbortController, reveal: boolean) {
    if (data.versionIdx !== request.versionIdx) throw new Error('Mismatched draw result');
    if (controller.signal.aborted) return;
    await clearDraw(userIdx, request.requestKey);
    if (!controller.signal.aborted) { setPending(undefined); setResult(data); setAnimate(reveal); setError(undefined); setRevision(n => n + 1); }
  }
  async function execute(request: DrawRequest, lookup: boolean, controller: AbortController) {
    try { await accept(await (lookup ? drawByRequest(request.requestKey, controller.signal) : createDraw(request, controller.signal)), request, controller, !lookup); }
    catch (e) {
      if (controller.signal.aborted) return; setError(e);
      // These refusals are definitive under the backend's user lock. Query the
      // original key before allowing the user to review and confirm a new version.
      if (!lookup && e instanceof ApiError && reviewCodes.has(e.code ?? '')) {
        try { await accept(await drawByRequest(request.requestKey, controller.signal), request, controller, false); }
        catch (lookupError) {
          if (controller.signal.aborted) return;
          if (lookupError instanceof ApiError && lookupError.code === 'NOT_FOUND') {
            await clearDraw(userIdx, request.requestKey);
            if (!controller.signal.aborted) { setPending(undefined); setState(undefined); setRevision(n => n + 1); }
          }
        }
      }
    }
  }
  async function run(work: (controller: AbortController) => Promise<void>) {
    if (operation.current && !operation.current.signal.aborted) return;
    const controller = new AbortController(); operation.current = controller; setBusy(true); setError(undefined);
    try { await work(controller); } catch (e) { if (!controller.signal.aborted) setError(e); }
    finally { if (!controller.signal.aborted) setBusy(false); controller.abort(); }
  }
  const points = state && /^(0|[1-9]\d*)$/.test(state.points) ? BigInt(state.points) : undefined;
  const enough = points !== undefined && points >= 100n;
  return <section className="draw-page"><Link to="/rewards">{t('rewards')}</Link><h1>{t('drawTitle')}</h1>
    {state && <><dl><dt>{t('taskPoints')}</dt><dd>{state.points}</dd><dt>{t('rewardCash')}</dt><dd>${state.rewardUsd}</dd></dl>
      {!state.available && <p>{t('drawUnavailable')}</p>}</>}
    {pending ? <div role="status"><p>{t('drawPending')}</p><Button disabled={busy} onClick={() => { void run(c => execute(pending, true, c)); }}>{t('drawLookup')}</Button><Button disabled={busy} onClick={() => { void run(c => execute(pending, false, c)); }}>{t('drawRetry')}</Button></div> : <>
      <Button color="primary" disabled={busy || !state?.available || !enough || loadError !== undefined} loading={busy} onClick={() => { void run(async controller => {
        if (!state?.available || !enough) return;
        const saved = await prepareDraw(userIdx, state.versionIdx); if (controller.signal.aborted) return;
        setPending(saved.request);
        if (!saved.existing) { setResult(undefined); await execute(saved.request, false, controller); }
      }); }}>{t('drawOnce')}</Button>
      {state?.available && points !== undefined && !enough && <p>{t('drawShortfall', { points: String(100n - points) })} <Link to="/tasks">{t('tasks')}</Link></p>}
    </>}
    {error !== undefined && <Failure error={error} />}{loadError !== undefined && <Failure error={loadError} />}
    {!state && loadError === undefined && <p role="status">{t('common:loading')}</p>}
    {result && <DrawResult data={result} animate={animate} />}
    <p><Link to="/draw-history">{t('drawHistory')}</Link> · <Button size="small" disabled={busy} onClick={() => setRevision(n => n + 1)}>{t('refreshBalances')}</Button></p>
    <p>{t('drawNotice')}</p>
    {state?.available && <details><summary>{t('drawRules')}</summary><p>{t('drawRuleVersion', { version: state.versionName, rtp: state.rtp })}</p>
      <table><thead><tr><th>{t('drawPrize')}</th><th>{t('rewardCash')}</th><th>{t('drawProbability')}</th></tr></thead><tbody>{state.prizes.map(prize => <tr key={prize.code}><td>{t(`drawPrize_${prize.code}`)}</td><td>${prize.rewardUsd}</td><td>{prize.weight / 100}%</td></tr>)}</tbody></table>
    </details>}
  </section>;
}
export function DrawHistory() { const auth = useAuth(); return <History key={auth.session.generation} />; }
function History() {
  const { t } = useTranslation(); const [before, setBefore] = useState(''); const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ data?: DrawListData; error?: unknown }>({});
  useEffect(() => { const c = new AbortController(); setState({}); void getDraws(before, c.signal).then(data => { if (!c.signal.aborted) setState({ data }); }, error => { if (!c.signal.aborted) setState({ error }); }); return () => c.abort(); }, [before, revision]);
  return <section className="draw-page"><Link to="/draws">{t('drawTitle')}</Link><h1>{t('drawHistory')}</h1>
    {state.data?.items.map(data => <DrawResult key={data.idx} data={data} />)}{state.data?.items.length === 0 && <p>{t('drawNoHistory')}</p>}
    {state.error !== undefined && <><Failure error={state.error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
    {!state.data && state.error === undefined && <p role="status">{t('common:loading')}</p>}
    {state.data?.nextCursor && <Button onClick={() => setBefore(state.data!.nextCursor)}>{t('taskNext')}</Button>}{before && <Button onClick={() => setBefore('')}>{t('taskFirst')}</Button>}
  </section>;
}
export function DrawDetail() { const { idx = '' } = useParams(); const auth = useAuth(); return <Detail key={`${auth.session.generation}:${idx}`} idx={idx} />; }
function Detail({ idx }: { idx: string }) {
  const { t } = useTranslation(); const [data, setData] = useState<DrawResultData>(); const [error, setError] = useState<unknown>(); const [revision, setRevision] = useState(0);
  useEffect(() => { const c = new AbortController(); setError(undefined); void getDraw(idx, c.signal).then(d => { if (!c.signal.aborted) setData(d); }, e => { if (!c.signal.aborted) setError(e); }); return () => c.abort(); }, [idx, revision]);
  return <section className="draw-page"><Link to="/draw-history">{t('drawHistory')}</Link>{data && <DrawResult data={data} />}
    {error !== undefined && <><Failure error={error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
    {!data && error === undefined && <p role="status">{t('common:loading')}</p>}
  </section>;
}
