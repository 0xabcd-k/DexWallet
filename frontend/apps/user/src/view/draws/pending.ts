import { isRecord } from '@dexwallet/api-client';
import type { DrawRequest } from '@dexwallet/api-contract/platform';
import { randomToken } from '../../app/random';
export const drawStoragePrefix = `dexwallet:user:pending-draw:${import.meta.env.VITE_API_BASE_URL ?? '/api'}:`;
export function readDraw(storage: Pick<Storage, 'getItem'>, userIdx: string): DrawRequest | undefined {
  const raw = storage.getItem(drawStoragePrefix + userIdx); if (!raw) return;
  const value: unknown = JSON.parse(raw);
  if (!isRecord(value) || typeof value.requestKey !== 'string' || !/^[A-Za-z0-9_-]{16,64}$/.test(value.requestKey) ||
    typeof value.versionIdx !== 'string' || !/^0\d{16,19}$/.test(value.versionIdx)) throw new Error('Invalid saved draw');
  return { requestKey: value.requestKey, versionIdx: value.versionIdx };
}
export async function prepareDraw(userIdx: string, versionIdx: string) {
  if (!navigator.locks) throw new Error('Draw persistence coordination unavailable');
  return navigator.locks.request(drawStoragePrefix + userIdx, () => {
    const existing = readDraw(localStorage, userIdx); if (existing) return { request: existing, existing: true };
    const request = { requestKey: randomToken(), versionIdx }; const raw = JSON.stringify(request);
    localStorage.setItem(drawStoragePrefix + userIdx, raw);
    if (localStorage.getItem(drawStoragePrefix + userIdx) !== raw) throw new Error('Draw persistence failed');
    return { request, existing: false };
  });
}
export async function clearDraw(userIdx: string, key: string) {
  if (!navigator.locks) throw new Error('Draw persistence coordination unavailable');
  await navigator.locks.request(drawStoragePrefix + userIdx, () => {
    if (readDraw(localStorage, userIdx)?.requestKey === key) localStorage.removeItem(drawStoragePrefix + userIdx);
  });
}
