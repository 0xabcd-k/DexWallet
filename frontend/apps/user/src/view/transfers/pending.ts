import { isRecord } from '@dexwallet/api-client';
import type { SubmitTransferRequest } from '@dexwallet/api-contract/platform';
import { randomToken } from '../../app/random';
import { cents } from '../../api/transfers';
export const pendingPrefix = 'dexwallet:user:pending-transfer:';
export type PendingTransfer = SubmitTransferRequest;
export function readPending(storage: Pick<Storage, 'getItem'>, idx: string): PendingTransfer | undefined {
  const raw = storage.getItem(pendingPrefix + idx);
  if (!raw) return;
  const d: unknown = JSON.parse(raw);
  if (!isRecord(d) || typeof d.requestKey !== 'string' || !/^[A-Za-z0-9_-]{16,64}$/.test(d.requestKey) ||
      typeof d.recipientIdx !== 'string' || !/^0\d{16,19}$/.test(d.recipientIdx) || typeof d.amount !== 'string' || (cents(d.amount) ?? 0n) <= 0n) throw new Error('Invalid saved transfer');
  return { requestKey: d.requestKey, recipientIdx: d.recipientIdx, amount: d.amount };
}
export function savePending(storage: Pick<Storage, 'getItem' | 'setItem'>, idx: string, target: string, amount: string): PendingTransfer {
  const existing = readPending(storage, idx);
  if (existing) return existing;
  const next = { requestKey: randomToken(), recipientIdx: target, amount };
  const serialized = JSON.stringify(next);
  storage.setItem(pendingPrefix + idx, serialized);
  if (storage.getItem(pendingPrefix + idx) !== serialized) throw new Error('Transfer persistence failed');
  return next;
}
export function clearPending(storage: Storage, idx: string, key: string) {
  if (readPending(storage, idx)?.requestKey === key) storage.removeItem(pendingPrefix + idx);
}
// The short lock covers durable intent creation only, never a network request.
export async function preparePending(idx: string, target: string, amount: string) {
  if (!navigator.locks) throw new Error('Durable transfer coordination unavailable');
  return navigator.locks.request(pendingPrefix + idx, () => { const existing = readPending(localStorage, idx); return { request: existing ?? savePending(localStorage, idx, target, amount), existing: !!existing }; });
}
