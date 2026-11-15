import { Link } from 'react-router';
import '../transfers/style.less';
import { Empty } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import Balances from '../balances';
import Records from '../records';
import { RewardOverview } from '../tasks/results';
export default function Home({ section }: { section: string }) {
  const { t } = useTranslation();
  return <section className="home-page"><p className="eyebrow">{t('title')}</p><h1>{t(section)}</h1>
    {section === 'wallet' && <><div className="home-actions"><Link to="/tasks">{t('tasks')}</Link><Link to="/receive">{t('receive')}</Link><Link to="/pay">{t('pay')}</Link></div><Link to="/balance?kind=usd_balance">{t('usdBalance')}</Link>{import.meta.env.NATIVE_APP && <p><Link to="/local-wallets">{t('localWallets')}</Link>{' · '}<Link to="/wallet-connections">{t('nativeWallets')}</Link></p>}</>}
    {section === 'rewards' && <><p><Link to="/tasks">{t('tasks')}</Link></p><p><Link to="/draws">{t('drawTitle')}</Link></p><p><Link to="/reward-transfers">{t('rtTitle')}</Link></p><RewardOverview /></>}
    {section === 'wallet' || section === 'rewards' ? <Balances section={section} /> : section === 'records' ? <Records /> :
      <div className="availability-card"><Empty description={t('common:unavailable')} /><p>{t('common:unavailableBody')}</p></div>}
  </section>;
}
