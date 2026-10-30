import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { Button } from 'antd-mobile';
import { formatAmount, formatTime, useTranslation } from '@dexwallet/i18n';
import type { AccountEntry } from '@dexwallet/api-contract/platform';
import { AccountKind } from '../../api/balances';
import { getEntries } from '../../api/entries';
import { Failure, useAuth } from '../../auth/context';
import './style.less';

const kindNames: Record<string, string> = { [AccountKind.usd]: 'usdBalance', [AccountKind.reward]: 'rewardCash', [AccountKind.points]: 'taskPoints' };
const sourceNames: Record<string, string> = { transfer: 'entryTransfer', deposit: 'entryDeposit', reward: 'entryReward' };

export default function Records() {
  const auth = useAuth(); const { t } = useTranslation();
  const [search, setSearch] = useSearchParams();
  const selected = search.get('kind') ?? '';
  const kind = Object.hasOwn(kindNames, selected) ? selected : '';
  const [revision, setRevision] = useState(0);
  if (!auth.session.credential) return <div className="balance-sign-in"><p>{t('signInRecords')}</p>
    <Link to={`/login?returnTo=${encodeURIComponent(`/records${search.size ? `?${search}` : ''}`)}`}>{t('signIn')}</Link></div>;
  if (auth.loading) return <p role="status">{t('common:loading')}</p>;
  if (auth.error) return <><Failure error={auth.error} /><Button onClick={auth.refresh}>{t('common:retry')}</Button></>;
  return <div className="account-records">
    <div className="records-controls"><label>{t('recordAccount')}<select value={kind} onChange={event => setSearch(event.target.value ? { kind: event.target.value } : {})}>
      <option value="">{t('allAccounts')}</option>
      {Object.entries(kindNames).map(([value, name]) => <option key={value} value={value}>{t(name)}</option>)}
    </select></label><Button size="small" onClick={() => setRevision(value => value + 1)}>{t('refreshRecords')}</Button></div>
    <p className="records-note">{t('recordsScope')}</p>
    <Statement key={`${auth.session.generation}:${kind}:${revision}`} kind={kind} />
  </div>;
}

function Statement({ kind }: { kind: string }) {
  const { t, i18n } = useTranslation();
  const [before, setBefore] = useState(''); const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<{ entries: AccountEntry[]; nextCursor: string; loading: boolean; error?: unknown }>({ entries: [], nextCursor: '', loading: true });
  useEffect(() => {
    const controller = new AbortController(); let active = true;
    setState(previous => ({ ...previous, loading: true, error: undefined }));
    void getEntries(kind, before, controller.signal).then(page => {
      if (active) setState(previous => ({ entries: before ? [...previous.entries, ...page.entries] : page.entries, nextCursor: page.nextCursor, loading: false }));
    }, error => {
      if (active) setState(previous => ({ ...previous, loading: false, error }));
    });
    return () => { active = false; controller.abort(); };
  }, [kind, before, attempt]);
  return <>
    {!state.loading && !state.error && state.entries.length === 0 && <p className="records-empty">{t('noAccountEntries')}</p>}
    <ol className="records-list" aria-label={t('accountEntries')}>
      {state.entries.map(entry => {
        const unit = entry.unit === 'POINT' ? t('pointsUnit') : entry.unit;
        return <li key={entry.idx}><article>
          <div className="record-title"><strong>{t(sourceNames[entry.businessType] ?? 'entryChange')}</strong><span>{t(kindNames[entry.kind]!)}</span></div>
          <p className={`record-amount record-${entry.direction}`}><span>{t(entry.direction === 'in' ? 'entryIn' : 'entryOut')}</span> {entry.direction === 'in' ? '+' : '−'}{formatAmount(entry.amount, unit)}</p>
          <dl><dt>{t('entryBalanceAfter')}</dt><dd>{formatAmount(entry.balanceAfter, unit)}</dd>
            <dt>{t('entryBusinessId')}</dt><dd>{entry.businessIdx}</dd>
            <dt>{t('common:time')}</dt><dd><time dateTime={entry.createdAt}>{formatTime(Date.parse(entry.createdAt), i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en')}</time></dd></dl>
        </article></li>;
      })}
    </ol>
    {state.loading && <p role="status">{t('common:loading')}</p>}
    {state.error ? <><Failure error={state.error} /><Button onClick={() => setAttempt(value => value + 1)}>{t('common:retry')}</Button></> :
      state.nextCursor && <Button disabled={state.loading} onClick={() => setBefore(state.nextCursor)}>{t('loadMoreRecords')}</Button>}
  </>;
}
