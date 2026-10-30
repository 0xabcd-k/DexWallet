import { createBrowserRouter, Navigate } from 'react-router';
import {
  DesktopLayout,
  DesktopError,
  DesktopNotFound,
  Unavailable,
} from '@dexwallet/ui-desktop';
import { useTranslation } from '@dexwallet/i18n';
import { createBackofficeClient, type Realm } from './client';
import { Provider, Guard, useAuth } from './auth';
import { Login } from './login';
import './style.less';
export { backofficeResources } from './locales';
function Loading() {
  const { t } = useTranslation();
  return <p role="status">{t('common:loading')}</p>;
}
function Overview() {
  const { t } = useTranslation();
  const auth = useAuth();
  return (
    <>
      <Unavailable title={t('overview')} />
      {auth.client.realm === 'merchant' && (
        <p className="desktop-page">{t('boPendingMerchant')}</p>
      )}
    </>
  );
}
export function createBackofficeRouter(
  realm: Realm,
  baseUrl: string,
  developmentRoutes: Parameters<typeof createBrowserRouter>[0] = [],
) {
  const client = createBackofficeClient(realm, baseUrl);
  const navigation = [
    { path: '/overview', labelKey: 'overview' },
    ...(realm === 'admin'
      ? [
          { path: '/accounts', labelKey: 'boAccounts' },
          { path: '/users', labelKey: 'boUsers' },
          { path: '/audits', labelKey: 'boAudits' },
        ]
      : []),
    { path: '/settings', labelKey: 'common:settings' },
  ];
  return createBrowserRouter([
    {
      element: <Provider client={client} />,
      hydrateFallbackElement: <Loading />,
      errorElement: <DesktopError />,
      children: [
        { path: '/login', element: <Login /> },
        {
          element: <Guard />,
          children: [
            {
              element: <DesktopLayout app={realm} navigation={navigation} />,
              children: [
                { index: true, element: <Navigate to="/overview" replace /> },
                { path: '/overview', element: <Overview /> },
                {
                  path: '/settings',
                  lazy: async () => ({
                    Component: (await import('./settings')).Settings,
                  }),
                },
                ...(realm === 'admin'
                  ? [
                      {
                        path: '/accounts',
                        lazy: async () => ({
                          Component: (await import('./accounts')).Accounts,
                        }),
                      },
                      {
                        path: '/users',
                        lazy: async () => ({
                          Component: (await import('./users')).Users,
                        }),
                      },
                      {
                        path: '/audits',
                        lazy: async () => ({
                          Component: (await import('./audits')).Audits,
                        }),
                      },
                    ]
                  : []),
              ],
            },
          ],
        },
        { path: '*', element: <DesktopNotFound /> },
        ...developmentRoutes,
      ],
    },
  ]);
}
