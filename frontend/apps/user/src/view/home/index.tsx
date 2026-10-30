import { Empty } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import Balances from '../balances';
export default function Home({ section }: { section: string }) {
  const { t } = useTranslation();
  return <section className="home-page"><p className="eyebrow">{t('title')}</p><h1>{t(section)}</h1>
    {section === 'wallet' || section === 'rewards' ? <Balances section={section} /> :
      <div className="availability-card"><Empty description={t('common:unavailable')} /><p>{t('common:unavailableBody')}</p></div>}
  </section>;
}
