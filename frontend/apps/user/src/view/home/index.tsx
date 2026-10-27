import { Empty } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
export default function Home({ section }: { section: string }) {
  const { t } = useTranslation();
  return <section className="home-page"><p className="eyebrow">{t('title')}</p><h1>{t(section)}</h1>
    <div className="availability-card"><Empty description={t('common:unavailable')} /><p>{t('common:unavailableBody')}</p></div>
  </section>;
}
