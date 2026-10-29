import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import { appResources } from './locales';
import { bootstrapTelegram, sessionStorageOrUndefined } from './app/telegram/bootstrap';
import { createUserRouter } from './app/router';
import { session } from './auth/session';
import { autoLoginAllowed } from './auth/context';

const navigation = performance.getEntriesByType('navigation')[0];
const navigationType = navigation instanceof PerformanceNavigationTiming ? navigation.type : 'navigate';
if (import.meta.env.DEV && import.meta.env.DEV_TELEGRAM) {
  const { injectTelegram } = await import('./dev/telegram');
  await injectTelegram(navigationType);
}
const context = await bootstrapTelegram({ href: location.href,
  navigationType, storage: sessionStorageOrUndefined() });
const i18n = await createAppI18n('user', appResources, context.language);
const root = document.getElementById('root');
if (!root) throw new Error('Missing root element');
if (context.mode === 'error') {
  root.textContent = i18n.t('common:startupError');
} else {
  if (context.source === 'fresh') history.replaceState(null, '', `${location.pathname}${location.search}#/wallet`);
  if (context.mode === 'telegram' && !session.capture().credential && autoLoginAllowed(context)) {
    const source = location.hash.slice(1);
    history.replaceState(null, '', `${location.pathname}${location.search}#/login?returnTo=${encodeURIComponent(source)}`);
  }
  const router = createUserRouter(context);
  createRoot(root).render(<StrictMode><I18nextProvider i18n={i18n}><RouterProvider router={router} /></I18nextProvider></StrictMode>);
}
