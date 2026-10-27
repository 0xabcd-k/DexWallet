import { isRecord } from '@dexwallet/api-client';
export function safeReturn(state: unknown, fallback = '/wallet'): string {
  if (!isRecord(state) || typeof state.from !== 'string') return fallback;
  const target = state.from;
  if (!/^\/(?!\/)/.test(target) || /[\\\r\n]/.test(target)) return fallback;
  const path = target.split('?')[0];
  return ['/wallet', '/rewards', '/records', '/__dev'].includes(path ?? '') ? target : fallback;
}
