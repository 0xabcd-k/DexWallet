import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import { backofficeResources } from '@dexwallet/backoffice';
import { appResources } from './locales';
import { router } from './app/router';
const i18n = await createAppI18n('merchant', {
  en: { ...appResources.en, ...backofficeResources.en },
  'zh-CN': { ...appResources['zh-CN'], ...backofficeResources['zh-CN'] },
});
const root = document.getElementById('root');
if (!root) throw new Error('Missing root element');
createRoot(root).render(
  <StrictMode>
    <I18nextProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nextProvider>
  </StrictMode>,
);
