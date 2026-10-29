import { isRecord } from '@dexwallet/api-client';
export function safeDestination(value: unknown, fallback = '/wallet'): string {
  if (typeof value !== 'string' || !/^\/(?!\/)/.test(value) || /[\\\r\n#]/.test(value)) return fallback;
  const path = value.split('?')[0];
  return ['/wallet', '/rewards', '/records', '/account', '/identities', '/settings', '/__dev'].includes(path ?? '') ? value : fallback;
}
export function safeReturn(state: unknown, fallback = '/wallet'): string {
  const target = safeDestination(isRecord(state) ? state.from : undefined, fallback);
  return target.split('?')[0] === '/settings' ? fallback : target;
}
