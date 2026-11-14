import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { Button } from 'antd-mobile';
import { formatTime, useTranslation } from '@dexwallet/i18n';
import type { TaskData, TaskListData, TaskCheckData } from '@dexwallet/api-contract/platform';
import { getTasks, getTask, checkTask } from '../../api/tasks';
import { Failure, useAuth } from '../../auth/context';
import './style.less';

export default function Tasks() {
  const auth = useAuth();
  return <TaskList key={auth.session.generation} />;
}
function TaskList() {
  const { t } = useTranslation();
  const [before, setBefore] = useState(''); const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ data?: TaskListData; error?: unknown }>({});
  useEffect(() => {
    const controller = new AbortController(); setState({});
    void getTasks(before, controller.signal).then(data => { if (!controller.signal.aborted) setState({ data }); }, error => { if (!controller.signal.aborted) setState({ error }); });
    return () => controller.abort();
  }, [before, revision]);
  return <section className="task-page"><h1>{t('tasks')}</h1>
    <p>{t('taskPreviewNotice')}</p>
    {state.error ? <><Failure error={state.error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></> : !state.data ? <p role="status">{t('common:loading')}</p> : <>
      {state.data.items.length === 0 && <p>{t('taskEmpty')}</p>}
      {state.data.items.map(item => <article className="task-card" key={item.idx}><h2><Link to={`/tasks/${item.idx}`}>{item.title}</Link></h2>
        <p>{item.description}</p><p>{t(`taskEligibility_${item.eligibility}`)}</p>
        {item.sample && <span className="task-sample">{t('taskSample')}</span>}
      </article>)}
      {state.data.nextCursor && <Button onClick={() => setBefore(state.data!.nextCursor)}>{t('taskNext')}</Button>}
      {before && <Button onClick={() => setBefore('')}>{t('taskFirst')}</Button>}
    </>}
  </section>;
}
export function TaskDetail() {
  const { idx = '' } = useParams(); const auth = useAuth();
  return <Detail key={`${auth.session.generation}:${idx}`} idx={idx} />;
}
function Detail({ idx }: { idx: string }) {
  const { t, i18n } = useTranslation(); const checkController = useRef<AbortController | undefined>(undefined);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ data?: TaskData; error?: unknown }>({});
  const [result, setResult] = useState<TaskCheckData>(); const [error, setError] = useState<unknown>(); const [checking, setChecking] = useState(false);
  useEffect(() => {
    const controller = new AbortController(); setState({}); setResult(undefined); setError(undefined);
    void getTask(idx, controller.signal).then(data => { if (!controller.signal.aborted) setState({ data }); }, failure => { if (!controller.signal.aborted) setState({ error: failure }); });
    return () => { controller.abort(); checkController.current?.abort(); };
  }, [idx, revision]);
  async function check() {
    if (checkController.current && !checkController.current.signal.aborted) return;
    const controller = new AbortController(); checkController.current = controller;
    setChecking(true); setError(undefined); setResult(undefined);
    try {
      const next = await checkTask(idx, controller.signal);
      if (!controller.signal.aborted) { setResult(next); setState({ data: next.task }); }
    } catch (failure) { if (!controller.signal.aborted) setError(failure); }
    finally { if (!controller.signal.aborted) setChecking(false); controller.abort(); }
  }
  const item = state.data;
  return <section className="task-page"><Link to="/tasks">{t('taskAll')}</Link>
    {state.error ? <><Failure error={state.error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></> : !item ? <p role="status">{t('common:loading')}</p> : <>
      <h1>{item.title}</h1><p>{item.description}</p>
      {item.sample && <p className="task-sample">{t('taskSampleNotice')}</p>}
      <dl><dt>{t('taskIdentity')}</dt><dd>Telegram</dd><dt>{t('taskStarts')}</dt><dd>{formatTime(Date.parse(item.startsAt), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en')}</dd><dt>{t('taskEnds')}</dt><dd>{formatTime(Date.parse(item.endsAt), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en')}</dd></dl>
      <p>{t(`taskEligibility_${item.eligibility}`)}</p>
      {item.eligibility === 'telegram_required' && <Link to="/settings" state={{ from: `/tasks/${idx}` }}>{t('taskBindTelegram')}</Link>}
      <Button color="primary" disabled={checking || item.eligibility !== 'eligible'} loading={checking} onClick={() => { void check(); }}>{t('taskCheck')}</Button>
      {error !== undefined && <Failure error={error} />}
      {result && <div role="status" className="task-result"><strong>{t(result.outcome === 'member' ? 'taskMember' : 'taskNotMember')}</strong><p>{t('taskCheckedAt', { time: formatTime(Date.parse(result.checkedAt), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en') })}</p></div>}
      <p>{t('taskPreviewNotice')}</p>
      <Button disabled={checking} onClick={() => setRevision(n => n + 1)}>{t('taskRefresh')}</Button>
    </>}
  </section>;
}
