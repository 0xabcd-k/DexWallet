import { createHashRouter, Navigate, Link, Outlet, useLocation } from 'react-router';
import { ConfigProvider, TabBar, Empty, Button } from 'antd-mobile';
import en from 'antd-mobile/es/locales/en-US';
import zh from 'antd-mobile/es/locales/zh-CN';
import { LanguageControl, useTranslation } from '@dexwallet/i18n';
import { safeReturn } from './navigation';
import Home from '../view/home';
import '../view/home/style.less';

function Loading() { const { t } = useTranslation('common'); return <p role="status">{t('loading')}</p>; }
function Theme() {
  const { i18n } = useTranslation();
  return <ConfigProvider locale={i18n.resolvedLanguage === 'zh-CN' ? zh : en}><div className="mobile-shell"><Outlet /></div></ConfigProvider>;
}
function MainLayout() {
  const { t } = useTranslation(); const location = useLocation();
  return <><header className="mobile-header"><strong>DexWallet</strong><Link to="/settings" state={{ from: location.pathname + location.search }}>{t('common:settings')}</Link></header>
    <main><Outlet /></main><nav className="bottom-nav" aria-label={t('navigation')}><TabBar activeKey={location.pathname}>
      {['wallet', 'rewards', 'records'].map(name => <TabBar.Item key={`/${name}`} title={<Link to={`/${name}`}>{t(name)}</Link>} />)}
    </TabBar></nav></>;
}
function ChildLayout() {
  const { t } = useTranslation('common'); const location = useLocation();
  return <><header className="mobile-header"><Link to={safeReturn(location.state)} replace>{t('back')}</Link><strong>{t('settings')}</strong></header><main><Outlet /></main></>;
}
function Settings() { const { t } = useTranslation('common'); return <section className="settings-panel"><h1>{t('settings')}</h1><LanguageControl app="user" /></section>; }
function NotFound() { const { t } = useTranslation('common'); return <main><Empty description={t('notFound')} /><Link to="/wallet"><Button>{t('home')}</Button></Link></main>; }
function RouteError() { const { t } = useTranslation('common'); return <main><h1>{t('pageError')}</h1><a href="/#/wallet">{t('home')}</a></main>; }
export function createUserRouter() {
  return createHashRouter([{ element: <Theme />, hydrateFallbackElement: <Loading />, errorElement: <RouteError />, children: [
    { element: <MainLayout />, children: [
      { index: true, element: <Navigate to="/wallet" replace /> },
      ...['wallet', 'rewards', 'records'].map(name => ({ path: `/${name}`, element: <Home section={name} /> })),
    ] },
    { element: <ChildLayout />, children: [
      { path: '/settings', element: <Settings /> },
      ...(import.meta.env.DEV ? [{ path: '/__dev/details', element: <Settings /> }] : []),
    ] },
    ...(import.meta.env.DEV ? [{ path: '/__dev', lazy: async () => { const { DevPage } = await import('../view/dev'); return { Component: DevPage }; } }] : []),
    { path: '*', element: <NotFound /> },
  ] }]);
}
