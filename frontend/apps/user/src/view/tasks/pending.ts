import { isRecord } from '@dexwallet/api-client';
import type { ClaimSubmission } from '../../api/task-rewards';
import { randomToken } from '../../app/random';

export const claimStoragePrefix = `dexwallet:user:pending-claim:${import.meta.env.VITE_API_BASE_URL ?? '/api'}:`;
const idx = (s: unknown) => typeof s === 'string' && /^0\d{16,19}$/.test(s);
export function readClaim(storage: Pick<Storage, 'getItem'>, userIdx: string): ClaimSubmission | undefined {
  const raw = storage.getItem(claimStoragePrefix + userIdx);
  if (!raw) return;
  const r: unknown = JSON.parse(raw);
  if (!isRecord(r) || !idx(r.idx) || typeof r.requestKey !== 'string' || !/^[A-Za-z0-9_-]{16,64}$/.test(r.requestKey) ||
      (r.mode !== 'self' && r.mode !== 'help') || (r.expectedParticipantStage !== 'first' && r.expectedParticipantStage !== 'returning') ||
      typeof r.expectedTaskRevision !== 'number' || !Number.isInteger(r.expectedTaskRevision) || Number(r.expectedTaskRevision) <= 0 ||
      typeof r.expectedIdentityRevision !== 'number' || !Number.isInteger(r.expectedIdentityRevision) || Number(r.expectedIdentityRevision) < 0 ||
      (r.mode === 'help' ? !idx(r.invitationIdx) : r.invitationIdx !== '')) throw new Error('Invalid saved claim');
  return { idx: String(r.idx), requestKey: r.requestKey, mode: r.mode, invitationIdx: String(r.invitationIdx), expectedTaskRevision: r.expectedTaskRevision, expectedIdentityRevision: r.expectedIdentityRevision, expectedParticipantStage: r.expectedParticipantStage };
}
export async function prepareClaim(userIdx: string, input: Omit<ClaimSubmission, 'requestKey'>) {
  if (!navigator.locks) throw new Error('Claim persistence coordination unavailable');
  return navigator.locks.request(claimStoragePrefix + userIdx, () => {
    const existing = readClaim(localStorage, userIdx);
    if (existing) return { request: existing, existing: true };
    const request = { ...input, requestKey: randomToken() };
    const raw = JSON.stringify(request);
    localStorage.setItem(claimStoragePrefix + userIdx, raw);
    if (localStorage.getItem(claimStoragePrefix + userIdx) !== raw) throw new Error('Claim persistence failed');
    return { request, existing: false };
  });
}
export async function clearClaim(userIdx: string, requestKey: string) {
  if (!navigator.locks) throw new Error('Claim persistence coordination unavailable');
  await navigator.locks.request(claimStoragePrefix + userIdx, () => {
    if (readClaim(localStorage, userIdx)?.requestKey === requestKey) localStorage.removeItem(claimStoragePrefix + userIdx);
  });
}
