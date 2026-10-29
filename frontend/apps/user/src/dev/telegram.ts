import { mockTelegramEnv } from '@tma.js/bridge';
import { isRecord } from '@dexwallet/api-client';
import { LAUNCH_STORAGE_KEY, sessionStorageOrUndefined } from '../app/telegram/bootstrap';

export async function injectTelegram(navigationType: string) {
  const params = new URLSearchParams(location.hash.slice(1));
  // Real launches and refreshes continue through the normal SDK recovery path.
  if (params.has('tgWebAppData') || params.has('tgWebAppVersion')) return;
  if ((navigationType === 'reload' || navigationType === 'back_forward') && sessionStorageOrUndefined()?.getItem(LAUNCH_STORAGE_KEY)) return;
  const response = await fetch('/__dev/telegram-launch', { method: 'POST', cache: 'no-store' });
  if (!response.ok) throw new Error('Telegram development launch unavailable');
  const body: unknown = await response.json();
  if (!isRecord(body) || typeof body.launchParams !== 'string') throw new Error('Invalid Telegram development launch');
  try { mockTelegramEnv({ launchParams: body.launchParams }); }
  catch { throw new Error('Invalid Telegram development launch'); }
  history.replaceState(null, '', `${location.pathname}${location.search}#${body.launchParams}`);
}
