import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { Button } from 'antd-mobile';
import { formatAmount, useTranslation } from '@dexwallet/i18n';
import type { AccountBalance } from '@dexwallet/api-contract/platform';
import { AccountKind, getBalances } from '../../api/balances';
import { Failure, useAuth } from '../../auth/context';
import './style.less';

export default function Balances({ section }: { section: 'wallet' | 'rewards' }) {
  const auth = useAuth(); const { t } = useTranslation();
  if (!auth.session.credential) return <div className="balance-sign-in"><p>{t('signInBalances')}</p>
    <Link to={`/login?returnTo=${encodeURIComponent(`/${section}`)}`}>{t('signIn')}</Link></div>;
  if (auth.loading) return <p role="status">{t('common:loading')}</p>;
  if (auth.error) return <><Failure error={auth.error} /><Button onClick={auth.refresh}>{t('common:retry')}</Button></>;
  return <AccountBalances key={`${auth.session.generation}:${section}`} section={section} />;
}

function AccountBalances({ section }: { section: 'wallet' | 'rewards' }) {
  const { t } = useTranslation();
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ accounts?: AccountBalance[]; error?: unknown }>({});
  useEffect(() => {
    const controller = new AbortController(); let active = true;
    setState({});
    void getBalances(controller.signal).then(accounts => {
      if (active) setState({ accounts });
    }, error => {
      if (active) setState({ error });
    });
    return () => { active = false; controller.abort(); };
  }, [revision]);
  const refresh = () => setRevision(value => value + 1);
  if (state.error) return <><Failure error={state.error} /><Button onClick={refresh}>{t('common:retry')}</Button></>;
  if (!state.accounts) return <p role="status">{t('common:loading')}</p>;
  const kinds: string[] = section === 'wallet' ? [AccountKind.usd] : [AccountKind.reward, AccountKind.points];
  const names: Record<string, string> = { [AccountKind.usd]: 'usdBalance', [AccountKind.reward]: 'rewardCash', [AccountKind.points]: 'taskPoints' };
  return <div className="balance-summary">
    {state.accounts.filter(account => kinds.includes(account.kind)).map(account => {
      const isUSD = account.kind === AccountKind.usd;
      const unit = account.kind === AccountKind.points ? t('pointsUnit') : account.unit;
      return <section className="balance-card" key={account.kind} aria-labelledby={`balance-${account.kind}`}>
        <h2 id={`balance-${account.kind}`}>{t(names[account.kind]!)}</h2>
        {isUSD && <p className="balance-label">{t('availableBalance')}</p>}
        <p className="balance-value">{formatAmount(account.balance, unit)}</p>
        {isUSD && <dl><dt>{t('heldBalance')}</dt><dd>{formatAmount(account.heldBalance, unit)}</dd>
          <dt>{t('totalBalance')}</dt><dd>{formatAmount(account.totalBalance, unit)}</dd></dl>}
        {isUSD && <p><Link to="/holds?status=held">{t('viewHolds')}</Link></p>}
        <Link to={`/records?kind=${encodeURIComponent(account.kind)}`}>{t('viewAccountEntries')}</Link>
      </section>;
    })}
    <Button className="balance-refresh" size="small" onClick={refresh}>{t('refreshBalances')}</Button>
    <p className="balance-note">{t(section === 'wallet' ? 'walletActionsPending' : 'rewardActionsPending')}</p>
  </div>;
}
