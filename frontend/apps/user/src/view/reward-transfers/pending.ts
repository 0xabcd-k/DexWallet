import { isRecord } from '@dexwallet/api-client';
import type { RewardTransferRequest } from '@dexwallet/api-contract/platform';
import { randomToken } from '../../app/random';
export const rewardTransferStoragePrefix = `dexwallet:user:pending-reward-transfer:${import.meta.env.VITE_API_BASE_URL ?? '/api'}:`;
export function readRewardTransfer(storage: Pick<Storage, 'getItem'>, userIdx: string): RewardTransferRequest | undefined {
  const raw = storage.getItem(rewardTransferStoragePrefix + userIdx); if (!raw) return;
  const value: unknown = JSON.parse(raw);
  if (!isRecord(value) || typeof value.requestKey !== 'string' || !/^[A-Za-z0-9_-]{16,64}$/.test(value.requestKey) ||
    typeof value.amount !== 'string' || !/^\d{1,18}\.\d{2}$/.test(value.amount)) throw new Error('Invalid saved rewardTransfer');
  return { requestKey: value.requestKey, amount: value.amount };
}
export async function prepareRewardTransfer(userIdx: string, amount: string) {
  if (!navigator.locks) throw new Error('RewardTransfer persistence coordination unavailable');
  return navigator.locks.request(rewardTransferStoragePrefix + userIdx, () => {
    const existing = readRewardTransfer(localStorage, userIdx); if (existing) return { request: existing, existing: true };
    const request = { requestKey: randomToken(), amount }; const raw = JSON.stringify(request);
    localStorage.setItem(rewardTransferStoragePrefix + userIdx, raw);
    if (localStorage.getItem(rewardTransferStoragePrefix + userIdx) !== raw) throw new Error('RewardTransfer persistence failed');
    return { request, existing: false };
  });
}
export async function clearRewardTransfer(userIdx: string, key: string) {
  if (!navigator.locks) throw new Error('RewardTransfer persistence coordination unavailable');
  await navigator.locks.request(rewardTransferStoragePrefix + userIdx, () => {
    if (readRewardTransfer(localStorage, userIdx)?.requestKey === key) localStorage.removeItem(rewardTransferStoragePrefix + userIdx);
  });
}
