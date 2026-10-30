import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router';
import { Button } from 'antd-mobile';
import { formatAmount, formatTime, useTranslation } from '@dexwallet/i18n';
import type { AccountHold } from '@dexwallet/api-contract/platform';
import { getHolds } from '../../api/holds';
import { Failure, useAuth } from '../../auth/context';
import '../records/style.less';

const statuses: Record<string, string> = { held: 'holdHeld', consumed: 'holdConsumed', released: 'holdReleased' };
const reasons: Record<string, string> = { completed: 'holdCompleted', cancelled: 'holdCancelled', rejected: 'holdRejected', not_executed: 'holdNotExecuted' };
export default function Holds() {
  const auth = useAuth(); const { t } = useTranslation();
  const [search, setSearch] = useSearchParams(); const selected = search.get('status') ?? '';
  const status = Object.hasOwn(statuses, selected) ? selected : '';
  const [revision, setRevision] = useState(0);
  return <div className="account-records"><p className="records-note">{t('holdsNote')}</p>
    <div className="records-controls"><label>{t('holdStatus')}<select value={status} onChange={e => setSearch(e.target.value ? { status: e.target.value } : {})}>
      <option value="">{t('holdAll')}</option>{Object.entries(statuses).map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}
    </select></label><Button size="small" onClick={() => setRevision(n => n + 1)}>{t('refreshHolds')}</Button></div>
    <HoldList key={`${auth.session.generation}:${status}:${revision}`} status={status} />
  </div>;
}
function HoldList({ status }: { status: string }) {
  const { t, i18n } = useTranslation();
  const [before, setBefore] = useState(''); const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<{ holds: AccountHold[]; nextCursor: string; loading: boolean; error?: unknown }>({ holds: [], nextCursor: '', loading: true });
  useEffect(() => {
    const controller = new AbortController(); let active = true;
    setState(previous => ({ ...previous, loading: true, error: undefined }));
    void getHolds(status, before, controller.signal).then(page => {
      if (active) setState(previous => ({ holds: before ? [...previous.holds, ...page.holds] : page.holds, nextCursor: page.nextCursor, loading: false }));
    }, error => { if (active) setState(previous => ({ ...previous, loading: false, error })); });
    return () => { active = false; controller.abort(); };
  }, [status, before, attempt]);
  const time = (value: string) => formatTime(Date.parse(value), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en');
  return <>
    {!state.loading && !state.error && !state.holds.length && <p className="records-empty">{t('noHolds')}</p>}
    <ol className="records-list" aria-label={t('holds')}>{state.holds.map(hold => <li key={hold.idx}><article>
      <div className="record-title"><strong>{t(hold.businessType === 'withdrawal' ? 'entryWithdrawal' : 'entryPayment')}</strong><span>{t(statuses[hold.status]!)}</span></div>
      <p className="record-amount">{formatAmount(hold.amount, hold.unit)}</p>
      <dl><dt>{t('entryBusinessId')}</dt><dd>{hold.businessIdx}</dd><dt>{t('common:time')}</dt><dd><time dateTime={hold.createdAt}>{time(hold.createdAt)}</time></dd>
        {hold.reason && <><dt>{t('holdReason')}</dt><dd>{t(reasons[hold.reason]!)}</dd><dt>{t('holdResolvedAt')}</dt><dd><time dateTime={hold.resolvedAt}>{time(hold.resolvedAt)}</time></dd></>}
      </dl></article></li>)}</ol>
    {state.loading && <p role="status">{t('common:loading')}</p>}
    {state.error ? <><Failure error={state.error} /><Button onClick={() => setAttempt(n => n + 1)}>{t('common:retry')}</Button></> : state.nextCursor && <Button disabled={state.loading} onClick={() => setBefore(state.nextCursor)}>{t('loadMoreRecords')}</Button>}
  </>;
}
