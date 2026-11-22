import { useEffect, useState } from 'react';
import { Link, Navigate, useParams, useSearchParams } from 'react-router';
import { Button } from 'antd-mobile';
import { formatTime, useTranslation } from '@dexwallet/i18n';
import type { RewardGrantData, TaskCompletionData, TaskCompletionListData, TaskInvitationData } from '@dexwallet/api-contract/platform';
import { getCompletion, getCompletions, getInvitation, getRewardGrant } from '../../api/task-rewards';
import { Failure, useAuth } from '../../auth/context';
import { useRewardSummary } from './use-reward-summary';

export function GrantResult({ data }: { data: RewardGrantData }) {
  const { t } = useTranslation();
  return <article className="task-card"><h3>{t(`claimReason_${data.reason}`)}</h3>
    <dl><dt>{t('rewardCash')}</dt><dd>${data.rewardUsd}</dd><dt>{t('taskPoints')}</dt><dd>{data.points}</dd>
      <dt>{t('claimProDelta')}</dt><dd>{data.proDelta}</dd><dt>{t('claimProAfter')}</dt><dd>{data.proAfter}</dd></dl>
    {data.unlockedPro && <p>{t('claimProUnlocked')}</p>}
    <Link to={data.reason === 'first_task' ? '/records?kind=reward_usd' : '/records?kind=task_points'}>{t('records')}</Link>
  </article>;
}
export function CompletionResult({ data }: { data: TaskCompletionData }) {
  const { t, i18n } = useTranslation();
  return <article className="task-result"><h2>{t('claimCompleted')}</h2><p>{data.taskTitle}</p>
    <p>{formatTime(Date.parse(data.completedAt), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en')}</p>
    {data.grants.length ? data.grants.map(grant => <GrantResult key={grant.idx} data={grant} />) : <p>{t('claimNoReward')}</p>}
    <Link to={`/task-completions/${data.idx}`}>{t('claimResult')}</Link>
  </article>;
}

export function CompletionHistory() {
  const auth = useAuth(); const [params] = useSearchParams(); const { t } = useTranslation();
  const taskIdx = params.get('taskIdx') ?? ''; const [before, setBefore] = useState(''); const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ data?: TaskCompletionListData; error?: unknown }>({});
  useEffect(() => {
    const controller = new AbortController(); setState({});
    void getCompletions(taskIdx, before, controller.signal).then(data => { if (!controller.signal.aborted) setState({ data }); }, error => { if (!controller.signal.aborted) setState({ error }); });
    return () => controller.abort();
  }, [taskIdx, before, revision, auth.session.generation]);
  return <section className="task-page"><h1>{t('claimHistory')}</h1><Link to="/tasks">{t('taskAll')}</Link>
    {state.data?.items.map(data => <CompletionResult key={data.idx} data={data} />)}
    {state.data?.items.length === 0 && <p>{t('claimNoHistory')}</p>}
    {state.error !== undefined && <><Failure error={state.error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
    {!state.data && state.error === undefined && <p role="status">{t('common:loading')}</p>}
    {state.data?.nextCursor && <Button onClick={() => setBefore(state.data!.nextCursor)}>{t('taskNext')}</Button>}
    {before && <Button onClick={() => setBefore('')}>{t('taskFirst')}</Button>}
  </section>;
}
export function CompletionDetail() {
  const auth = useAuth(); const { idx = '' } = useParams();
  return <StoredResult key={`${auth.session.generation}:${idx}`} idx={idx} grant={false} />;
}
export function RewardGrantDetail() {
  const auth = useAuth(); const { idx = '' } = useParams();
  return <StoredResult key={`${auth.session.generation}:${idx}`} idx={idx} grant />;
}
function StoredResult({ idx, grant }: { idx: string; grant: boolean }) {
  const { t } = useTranslation(); const [data, setData] = useState<TaskCompletionData>(); const [award, setAward] = useState<RewardGrantData>();
  const [error, setError] = useState<unknown>(); const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); setError(undefined);
    const work = grant ? getRewardGrant(idx, controller.signal).then(d => { if (!controller.signal.aborted) setAward(d); }) : getCompletion(idx, controller.signal).then(d => { if (!controller.signal.aborted) setData(d); });
    void work.catch(e => { if (!controller.signal.aborted) setError(e); });
    return () => controller.abort();
  }, [idx, grant, revision]);
  return <section className="task-page"><Link to="/tasks">{t('taskAll')}</Link>{data && <CompletionResult data={data} />}{award && <><h2>{award.taskTitle}</h2><GrantResult data={award} /></>}
    {error !== undefined && <><Failure error={error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
    {!data && !award && error === undefined && <p role="status">{t('common:loading')}</p>}</section>;
}
export function InvitationLanding() {
  const { idx = '' } = useParams(); const auth = useAuth(); const { t } = useTranslation();
  const [data, setData] = useState<TaskInvitationData>(); const [error, setError] = useState<unknown>();
  useEffect(() => { const controller = new AbortController(); setData(undefined); setError(undefined);
    void getInvitation(idx, controller.signal).then(d => { if (!controller.signal.aborted) setData(d); }, e => { if (!controller.signal.aborted) setError(e); });
    return () => controller.abort();
  }, [idx, auth.session.generation]);
  // Build an internal destination from validated idx values, never redirect to an API-supplied URL.
  if (data && /^0\d{16,19}$/.test(data.taskIdx) && /^0\d{16,19}$/.test(data.idx)) return <Navigate to={`/tasks/${data.taskIdx}?invitationIdx=${data.idx}`} replace />;
  return <section className="task-page">{error !== undefined ? <Failure error={error} /> : <p role="status">{t('common:loading')}</p>}<Link to="/tasks">{t('taskAll')}</Link></section>;
}
export function RewardOverview() {
  const auth = useAuth();
  return auth.session.credential ? <Summary key={auth.session.generation} /> : null;
}
function Summary() {
  const { t } = useTranslation();
  const [revision, setRevision] = useState(0);
  const { data, error } = useRewardSummary(revision);
  return <section className="task-card"><h2>{t('claimRewardOverview')}</h2><Link to="/invitations">{t('inviteResults')}</Link>{data && <>
    <p>{t(data.pro ? 'claimProUnlocked' : 'claimProProgress', { progress: data.proProgress })}</p>
    <p>{t('claimProRules')}</p><p>{t('claimInviteTotals', { friends: data.friendCount, helps: data.assistanceCount })}</p>
    <p>{t('claimEarnedTotals', { assistance: data.assistancePoints, commission: data.commissionPoints })}</p>
    {!data.firstCompleted && <Link to="/tasks">{t('claimFirstGuide')}</Link>}</>}
    {error !== undefined && <Failure error={error} />}
    <Button onClick={() => setRevision(n => n + 1)}>{t('taskRefresh')}</Button><Link to="/task-completions">{t('claimHistory')}</Link>
  </section>;
}
