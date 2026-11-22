import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { Button } from 'antd-mobile';
import { decodeData } from '@dexwallet/api-client';
import type { InvitationEarningsData, ReferralFriendsData } from '@dexwallet/api-contract/platform';
import { useTranslation } from '@dexwallet/i18n';
import { useRewardSummary } from './use-reward-summary';
import './invitation-results.less';
import { userRequest } from '../../api/account';
import { Failure, useAuth } from '../../auth/context';
export default function InvitationResults() {
    const { session } = useAuth();
    return <Results key={session.generation}/>;
}
function Results() {
    const { t } = useTranslation();
    const [tab, setTab] = useState<'friends' | 'earnings'>('earnings');
    const [before, setBefore] = useState('');
    const [revision, setRevision] = useState(0);
    const summary = useRewardSummary(revision);
    const [friendIdx, setFriendIdx] = useState('');
    const [reason, setReason] = useState('all');
    const [from, setFrom] = useState('');
    const [to, setTo] = useState('');
    const [filter, setFilter] = useState({ friendIdx: '', reason: 'all', from: '', to: '' });
    const [earnings, setEarnings] = useState<InvitationEarningsData>();
    const [friends, setFriends] = useState<ReferralFriendsData>();
    const [error, setError] = useState<unknown>();
    useEffect(() => {
        const c = new AbortController();
        setError(undefined);
        setEarnings(undefined);
        setFriends(undefined);
        const work = tab === 'friends' ? userRequest(`/v1/user/referral-friends?${new URLSearchParams({ before, limit: '20' })}`, decodeData<ReferralFriendsData>, { signal: c.signal }).then(d => { if (!c.signal.aborted)
            setFriends(d); }) : userRequest(`/v1/user/invitation-earnings?${new URLSearchParams({ ...filter, before, limit: '20' })}`, decodeData<InvitationEarningsData>, { signal: c.signal }).then(d => { if (!c.signal.aborted)
            setEarnings(d); });
        void work.catch(e => { if (!c.signal.aborted)
            setError(e); });
        return () => c.abort();
    }, [tab, before, filter, revision]);
    const next = tab === 'friends' ? friends?.nextCursor : earnings?.nextCursor;
    return <section className="task-page invitation-results"><h1>{t('inviteResults')}</h1>
    <section className="invitation-summary" aria-labelledby="invitation-summary-title">
      <h2 id="invitation-summary-title">{t('inviteLifetime')}</h2>
      {summary.data && <dl>
        <div><dt>{t('inviteBoundFriends')}</dt><dd>{summary.data.friendCount}</dd></div>
        <div><dt>{t('inviteValidHelps')}</dt><dd>{summary.data.assistanceCount}</dd></div>
        <div><dt>{t('inviteHelpPoints')}</dt><dd><span aria-hidden="true">☆</span>{summary.data.assistancePoints}</dd></div>
        <div><dt>{t('inviteCommissionPoints')}</dt><dd><span aria-hidden="true">☆</span>{summary.data.commissionPoints}</dd></div>
      </dl>}
      {summary.error !== undefined && <Failure error={summary.error}/>}
      {!summary.data && summary.error === undefined && <p role="status">{t('common:loading')}</p>}
    </section>
    <p>{t('inviteBoundary')}</p>
    <nav className="invitation-links"><Link to="/rewards">{t('claimRewardOverview')}</Link><Link to="/tasks">{t('inviteChooseTask')}</Link><Link to="/task-completions">{t('claimHistory')}</Link></nav>
    <div role="tablist" aria-label={t('inviteResults')}>{(['earnings', 'friends'] as const).map(v => <button type="button" key={v} role="tab" aria-selected={tab === v} onClick={() => { setTab(v); setBefore(''); }}>{t(`invite_${v}`)}</button>)}</div>
    {tab === 'earnings' && <form onSubmit={e => { e.preventDefault(); setBefore(''); setFilter({ friendIdx: friendIdx.trim(), reason, from, to }); }} className="task-card">
      <label>{t('inviteFriendIdx')}<input value={friendIdx} onChange={e => setFriendIdx(e.target.value)}/></label>
      <label>{t('inviteType')}<select value={reason} onChange={e => setReason(e.target.value)}>{['all', 'assistance', 'commission'].map(r => <option key={r} value={r}>{t(r === 'all' ? 'inviteAll' : `claimReason_${r}`)}</option>)}</select></label>
      <label>{t('inviteFrom')}<input type="date" value={from} onChange={e => setFrom(e.target.value)}/></label><label>{t('inviteTo')}<input type="date" value={to} min={from} onChange={e => setTo(e.target.value)}/></label><Button type="submit">{t('inviteFilter')}</Button>
    </form>}
    {friends?.items.map(f => <article className="task-card" key={f.userIdx}><h2>{f.nickname || f.userIdx}</h2><p>{f.userIdx}</p><p>{t('inviteBoundAt')}: {new Date(f.boundAt).toLocaleString()}</p><Button onClick={() => { setFriendIdx(f.userIdx); setFilter({ ...filter, friendIdx: f.userIdx }); setTab('earnings'); setBefore(''); }}>{t('inviteViewIncome')}</Button></article>)}
    {earnings?.items.map(e => <article className="task-card" key={e.grantIdx}><h2>{t(`claimReason_${e.reason}`)} · +{e.points} {t('taskPoints')}</h2><p>{e.peerNickname || e.peerIdx} · {e.peerIdx}</p><p>{e.taskTitle}</p><p>{new Date(e.createdAt).toLocaleString()}</p><Link to={`/reward-grants/${e.grantIdx}`}>{t('claimResult')}</Link></article>)}
    {(friends?.items.length === 0 || earnings?.items.length === 0) && <p>{t(tab === 'friends' ? 'inviteNoFriends' : filter.friendIdx || filter.reason !== 'all' || filter.from || filter.to ? 'inviteEmpty' : 'inviteNoEarnings')}</p>}
    {!friends && !earnings && error === undefined && <p role="status">{t('common:loading')}</p>}{error !== undefined && <Failure error={error}/>}
    <Button onClick={() => setRevision(n => n + 1)}>{t('taskRefresh')}</Button>{next && <Button onClick={() => setBefore(next)}>{t('taskNext')}</Button>}{before && <Button onClick={() => setBefore('')}>{t('taskFirst')}</Button>}
  </section>;
}
