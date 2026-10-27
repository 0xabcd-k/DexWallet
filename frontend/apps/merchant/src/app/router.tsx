import { createBrowserRouter, Navigate } from 'react-router';
import { DesktopLayout, DesktopError, DesktopNotFound } from '@dexwallet/ui-desktop';
import { LanguageControl, useTranslation } from '@dexwallet/i18n';
import Overview from '../view/overview';
function Settings() { const { t } = useTranslation('common'); return <section className="desktop-page"><h1>{t('settings')}</h1><LanguageControl app="merchant" /></section>; }
function Loading() { const { t } = useTranslation('common'); return <p role="status">{t('loading')}</p>; }
export const router = createBrowserRouter([{
  hydrateFallbackElement: <Loading />, element: <DesktopLayout app="merchant" navigation={[{ path: '/overview', labelKey: 'overview' }, { path: '/settings', labelKey: 'common:settings' }]} />, errorElement: <DesktopError />,
  children: [{ index: true, element: <Navigate to="/overview" replace /> },
    { path: '/overview', element: <Overview /> }, { path: '/settings', element: <Settings /> },
    ...(import.meta.env.DEV ? [{ path: '/__dev', lazy: async () => { const { DevPage } = await import('../view/dev'); return { Component: DevPage }; } }] : []),
    { path: '*', element: <DesktopNotFound /> }],
}]);
