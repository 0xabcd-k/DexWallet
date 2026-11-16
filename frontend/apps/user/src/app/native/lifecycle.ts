import { receiveFields, chainPreparationRoute } from '../chain-intent';
import { App } from '@capacitor/app';
import type { createUserRouter } from '../router';
import type { LaunchContext } from '../telegram/bootstrap';
import { session } from '../../auth/session';
export const APP_RETURN_URL = 'com.dexwallet.app://wallet-return';
export const nativeLaunch: LaunchContext = { mode: 'browser', source: 'none', fingerprint: '', launchId: '', rawInitData: '', target: null, targetStatus: 'consumed', storageAvailable: true };
// Receive links only prepare a destination. Wallet callbacks carry no trusted
// result; TON Connect independently authenticates bridge events.
export function nativeDestination(value: string): string | undefined {
  if (receiveFields(value)) return chainPreparationRoute(value);
  // Exact callbacks also work in WebViews that treat custom schemes as opaque.
  if (value === APP_RETURN_URL || value === `${APP_RETURN_URL}/`) return '/wallet-connections';
}
export async function installNativeLifecycle(router: ReturnType<typeof createUserRouter>, app = App) {
  const open = (url: string) => { const target = nativeDestination(url); if (target) return router.navigate(target); };
  await app.addListener('appUrlOpen', event => { void open(event.url); });
  await app.addListener('appStateChange', ({ isActive }) => { if (isActive) session.sync(); });
  await app.addListener('backButton', ({ canGoBack }) => { if (canGoBack && router.state.location.pathname !== '/wallet') void router.navigate(-1); else void app.minimizeApp(); });
  const launch = await app.getLaunchUrl();
  // Resolve the lazy route before the initial index redirect can be rendered.
  if (launch?.url) await open(launch.url);
}
