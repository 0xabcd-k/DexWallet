import { Link } from 'react-router';
import '../transfers/style.less';
import { Empty } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import Balances from '../balances';
import Records from '../records';
export default function Home({ section }: { section: string }) {
  const { t } = useTranslation();
  return <section className="home-page"><p className="eyebrow">{t('title')}</p><h1>{t(section)}</h1>
    {section === 'wallet' && <><div className="home-actions"><Link to="/tasks">{t('tasks')}</Link><Link to="/receive">{t('receive')}</Link><Link to="/pay">{t('pay')}</Link></div><Link to="/balance?kind=usd_balance">{t('usdBalance')}</Link></>}
    {section === 'rewards' && <p><Link to="/tasks">{t('tasks')}</Link></p>}
    {section === 'wallet' || section === 'rewards' ? <Balances section={section} /> : section === 'records' ? <Records /> :
      <div className="availability-card"><Empty description={t('common:unavailable')} /><p>{t('common:unavailableBody')}</p></div>}
  </section>;
}
