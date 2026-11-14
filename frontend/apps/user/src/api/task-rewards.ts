import { decodeData, isRecord, type Decoder } from '@dexwallet/api-client';
import type { ClaimPreviewData, TaskClaimRequest, TaskCompletionData, TaskCompletionListData, TaskInvitationData, RewardSummaryData, RewardGrantData } from '@dexwallet/api-contract/platform';
import { userRequest } from './account';

const validIdx = (v: unknown): v is string => typeof v === 'string' && /^0\d{16,19}$/.test(v);
const whole = (v: unknown) => typeof v === 'string' && /^(0|[1-9]\d*)$/.test(v);
function validGrant(v: unknown): boolean {
  return isRecord(v) && validIdx(v.idx) && validIdx(v.taskIdx) && validIdx(v.completionIdx) && validIdx(v.voucherIdx) &&
    ['first_task', 'own_task', 'assistance', 'commission'].includes(String(v.reason)) &&
    typeof v.rewardUsd === 'string' && /^(0|[1-9]\d*)\.\d{2}$/.test(v.rewardUsd) &&
    whole(v.points) && whole(v.proDelta) && whole(v.proBefore) && whole(v.proAfter) &&
    typeof v.wasPro === 'boolean' && typeof v.unlockedPro === 'boolean';
}
export const decodeCompletion: Decoder<TaskCompletionData> = value => {
  if (!isRecord(value) || !isRecord(value.data)) throw new TypeError('Missing completion');
  const d = value.data;
  if (!validIdx(d.idx) || !validIdx(d.taskIdx) || typeof d.taskTitle !== 'string' ||
      !['self', 'help'].includes(String(d.mode)) || !['first', 'returning'].includes(String(d.participantStage)) ||
      typeof d.completedAt !== 'string' || !Number.isFinite(Date.parse(d.completedAt)) ||
      !Array.isArray(d.grants) || !d.grants.every(validGrant)) throw new TypeError('Invalid completion');
  return decodeData<TaskCompletionData>(value);
};
export const getClaimPreview = (idx: string, invitationIdx: string, signal: AbortSignal) => userRequest(`/v1/user/tasks/${encodeURIComponent(idx)}/claim-preview?${new URLSearchParams({ invitationIdx })}`, decodeData<ClaimPreviewData>, { signal });
export type ClaimSubmission = TaskClaimRequest & { idx: string };
export const submitClaim = (r: ClaimSubmission, signal: AbortSignal) => userRequest(`/v1/user/tasks/${encodeURIComponent(r.idx)}/claim`, decodeCompletion, { method: 'POST', body: r, signal });
export const claimByRequest = (key: string, signal: AbortSignal) => userRequest(`/v1/user/task-completions/by-request/${encodeURIComponent(key)}`, decodeCompletion, { signal });
export const getCompletion = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/task-completions/${encodeURIComponent(idx)}`, decodeCompletion, { signal });
export const getCompletions = (taskIdx: string, before: string, signal: AbortSignal) => userRequest(`/v1/user/task-completions?${new URLSearchParams({ taskIdx, before, limit: '20' })}`, decodeData<TaskCompletionListData>, { signal });
export const createInvitation = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/tasks/${encodeURIComponent(idx)}/invitation`, decodeData<TaskInvitationData>, { method: 'POST', signal });
export const getInvitation = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/task-invitations/${encodeURIComponent(idx)}`, decodeData<TaskInvitationData>, { signal });
export const getRewardSummary = (signal: AbortSignal) => userRequest('/v1/user/reward-summary', decodeData<RewardSummaryData>, { signal });
export const getRewardGrant = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/reward-grants/${encodeURIComponent(idx)}`, decodeData<RewardGrantData>, { signal });
