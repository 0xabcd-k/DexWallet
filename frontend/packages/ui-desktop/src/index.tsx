import { ConfigProvider, Layout, Menu, Result, Button } from 'antd';
import en from 'antd/locale/en_US';
import zh from 'antd/locale/zh_CN';
import { Link, Outlet, useLocation } from 'react-router';
import { LanguageControl, useTranslation, type AppName } from '@dexwallet/i18n';
import './style.less';
export function DesktopLayout({ app, navigation }: { app: AppName; navigation: readonly { path: string; labelKey: string }[] }) {
  const { t, i18n } = useTranslation();
  const location = useLocation();
  return <ConfigProvider locale={i18n.resolvedLanguage === 'zh-CN' ? zh : en} theme={{ token: {
    colorPrimary: '#206657', colorTextDescription: '#596b65', borderRadius: 10, fontFamily: 'Inter, system-ui, sans-serif',
  } }}><Layout className="desktop-shell">
    <Layout.Sider width={232} breakpoint="lg" collapsedWidth={72}>
      <div className="brand">DexWallet</div>
      <nav aria-label={t('navigation')}><Menu theme="dark" selectedKeys={[location.pathname]} items={navigation.map(item => ({ key: item.path, label: <Link to={item.path}>{t(item.labelKey)}</Link> }))} /></nav>
    </Layout.Sider>
    <Layout><Layout.Header><span>{t('title')}</span><LanguageControl app={app} /></Layout.Header>
      <Layout.Content><Outlet /></Layout.Content>
    </Layout>
  </Layout></ConfigProvider>;
}
export function Unavailable({ title }: { title: string }) {
  const { t } = useTranslation('common');
  return <section className="desktop-page"><h1>{title}</h1><Result status="info" title={t('unavailable')} subTitle={t('unavailableBody')} /></section>;
}
export function DesktopNotFound() {
  const { t } = useTranslation('common');
  return <Result status="404" title={t('notFound')} subTitle={t('notFoundBody')} extra={<Link to="/overview"><Button>{t('home')}</Button></Link>} />;
}
export function DesktopError() {
  const { t } = useTranslation('common');
  return <Result status="error" title={t('pageError')} extra={<a href="/overview">{t('home')}</a>} />;
}
