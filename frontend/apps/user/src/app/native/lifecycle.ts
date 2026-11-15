import { App } from '@capacitor/app';
import type { createUserRouter } from '../router';
import type { LaunchContext } from '../telegram/bootstrap';
import { session } from '../../auth/session';
export const APP_RETURN_URL = 'com.dexwallet.app://wallet-return';
export const nativeLaunch: LaunchContext = { mode: 'browser', source: 'none', fingerprint: '', launchId: '', rawInitData: '', target: null, targetStatus: 'consumed', storageAvailable: true };
// A return link is navigation only. No address, token or transaction result is
// taken from its payload; TON Connect independently authenticates bridge events.
export function nativeDestination(value: string): string | undefined {
  try {
    const url = new URL(value);
    if (url.protocol === 'com.dexwallet.app:' && url.host === 'wallet-return' && !url.username && !url.password && !url.search && !url.hash && (url.pathname === '' || url.pathname === '/')) return '/wallet-connections';
  } catch { /* Ignore foreign and malformed intents. */ }
}
export async function installNativeLifecycle(router: ReturnType<typeof createUserRouter>) {
  const open = (url: string) => { const target = nativeDestination(url); if (target) void router.navigate(target); };
  await App.addListener('appUrlOpen', event => open(event.url));
  await App.addListener('appStateChange', ({ isActive }) => { if (isActive) session.sync(); });
  await App.addListener('backButton', ({ canGoBack }) => { if (canGoBack && router.state.location.pathname !== '/wallet') void router.navigate(-1); else void App.minimizeApp(); });
  const launch = await App.getLaunchUrl();
  if (launch?.url) open(launch.url);
}
