import { describe, expect, it } from 'vitest';
import { decodeCompletion } from '../../apps/user/src/api/task-rewards';
import { claimStoragePrefix, readClaim } from '../../apps/user/src/view/tasks/pending';
import { taskInvitationTarget, isReceiveTarget } from '../../apps/user/src/app/telegram/bootstrap';

const taskIdx = '03333333333333333';
describe('claim recovery boundary', () => {
  it('restores the original identity, task and invitation context, isolated by product user', () => {
    const storage = new Map<string, string>();
    const original = { idx: taskIdx, requestKey: 'original_claim_request', mode: 'help', invitationIdx: '04444444444444444', expectedTaskRevision: 2, expectedIdentityRevision: 3, expectedParticipantStage: 'first' };
    storage.set(claimStoragePrefix + 'user1', JSON.stringify(original));
    const reader = { getItem: (k: string) => storage.get(k) ?? null };
    expect(readClaim(reader, 'user1')).toEqual(original);
    expect(readClaim(reader, 'user2')).toBeUndefined();
    storage.set(claimStoragePrefix + 'user1', JSON.stringify({ ...original, expectedIdentityRevision: -1 }));
    expect(() => readClaim(reader, 'user1')).toThrow();
    storage.set(claimStoragePrefix + 'user1', JSON.stringify({ ...original, mode: 'self' }));
    expect(() => readClaim(reader, 'user1')).toThrow();
  });
  it('does not accept malformed settlement data as a completed claim', () => {
    const data = { idx: '05555555555555555', taskIdx, taskTitle: 'Sample', mode: 'help', participantStage: 'returning', completedAt: '2026-11-14T00:00:00Z', grants: [] };
    expect(decodeCompletion({ data })).toEqual(data);
    expect(() => decodeCompletion({ data: { ...data, grants: [{ points: '-1' }] } })).toThrow();
    expect(() => decodeCompletion({ data: { ...data, completedAt: 'unknown' } })).toThrow();
  });
  it('recognizes only task-specific invitation starts and keeps receive starts separate', () => {
    expect(taskInvitationTarget('task_' + taskIdx)).toBe(taskIdx);
    expect(isReceiveTarget('task_' + taskIdx)).toBe(false);
    expect(taskInvitationTarget('task_https://example.com')).toBeUndefined();
    expect(taskInvitationTarget(null)).toBeUndefined();
  });
});
