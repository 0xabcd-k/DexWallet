import { decodeData, isRecord, type Decoder } from '@dexwallet/api-client';
import type { RewardTransferData, RewardTransferStateData, RewardTransferListData, RewardTransferRequest } from '@dexwallet/api-contract/platform';
import { userRequest } from './account';
export const MinimumRewardCents = 200n;
export function usdCents(value: string): bigint | undefined {
  if (!/^(0|[1-9]\d{0,17})(\.\d{1,2})?$/.test(value)) return;
  const [whole, part = ''] = value.split('.'); return BigInt(whole!) * 100n + BigInt(part.padEnd(2, '0'));
}
export const decodeRewardTransfer: Decoder<RewardTransferData> = value => {
  if (!isRecord(value) || !isRecord(value.data)) throw new TypeError('Missing reward transfer');
  const d = value.data;
  if (typeof d.idx !== 'string' || !/^0\d{16,19}$/.test(d.idx) || typeof d.userIdx !== 'string' || !/^0\d{16,19}$/.test(d.userIdx) ||
    typeof d.amount !== 'string' || (usdCents(d.amount) ?? 0n) < MinimumRewardCents || d.fee !== '0.00' || !['pending_review','completed','rejected'].includes(String(d.status)) ||
    typeof d.reviewReason !== 'string' || typeof d.createdAt !== 'string' || !Number.isFinite(Date.parse(d.createdAt)) ||
    (d.status !== 'pending_review' && (typeof d.resolvedAt !== 'string' || !Number.isFinite(Date.parse(d.resolvedAt))))) throw new TypeError('Invalid reward transfer');
  return decodeData<RewardTransferData>(value);
};
const path = '/v1/user/reward-transfers';
export const getRewardTransferState = (signal: AbortSignal) => userRequest('/v1/user/reward-transfer-state', decodeData<RewardTransferStateData>, { signal });
export const submitRewardTransfer = (body: RewardTransferRequest, signal: AbortSignal) => userRequest(path, decodeRewardTransfer, { method: 'POST', body, signal });
export const rewardTransferByRequest = (key: string, signal: AbortSignal) => userRequest(`${path}/by-request/${encodeURIComponent(key)}`, decodeRewardTransfer, { signal });
export const getRewardTransfer = (idx: string, signal: AbortSignal) => userRequest(`${path}/${encodeURIComponent(idx)}`, decodeRewardTransfer, { signal });
export const getRewardTransfers = (before: string, signal: AbortSignal) => userRequest(`${path}?${new URLSearchParams({ before, limit: '20' })}`, decodeData<RewardTransferListData>, { signal });
