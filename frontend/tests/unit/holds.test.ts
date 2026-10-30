import { expect, it } from 'vitest';
import { decodeHolds } from '../../apps/user/src/api/holds';
import { decodeEntries } from '../../apps/user/src/api/entries';
const held = { idx: 'hold', businessType: 'withdrawal', businessIdx: 'business', amount: '9007199254740993.01', unit: 'USD', status: 'held', reason: '', holdVoucherIdx: 'voucher', resolvedVoucherIdx: '', createdAt: '2026-10-30T08:00:00Z', resolvedAt: '' };
const decode = (row: unknown) => decodeHolds({ data: { holds: [row], nextCursor: '' } });
it('preserves exact held amounts and distinguishes completed and released funds', () => {
  expect(decode(held).holds[0]).toEqual(held);
  for (const [status, reason] of [['consumed', 'completed'], ['released', 'rejected']]) {
    const resolved = { ...held, status, reason, resolvedVoucherIdx: 'resolved', resolvedAt: '2026-10-30T09:00:00Z' };
    expect(decode(resolved).holds[0]).toEqual(resolved);
  }
});
it('rejects impossible reservation states and invalid amounts', () => {
  for (const change of [{ amount: 10 }, { amount: '1.001' }, { amount: '0.00' }, { amount: '-1.00' }, { status: 'unknown' }, { status: 'consumed' }, { reason: 'completed' }, { unit: 'POINT' }, { businessType: 'reward' }, { resolvedVoucherIdx: 'already-done' }, { createdAt: 'bad' }]) expect(() => decode({ ...held, ...change })).toThrow();
  expect(() => decodeHolds({ data: { holds: [] } })).toThrow();
});
it('does not interpret internal reservations as ordinary income or expenditure', () => {
  const row = { idx: 'entry', voucherIdx: 'voucher', businessIdx: 'business', businessType: 'withdrawal', action: 'hold', kind: 'usd_balance', unit: 'USD', direction: 'out', amount: '30.00', balanceAfter: '70.00', createdAt: '2026-10-30T08:00:00Z', balancePart: 'available', activity: 'held' };
  const read = (changes: object) => decodeEntries({ data: { entries: [{ ...row, ...changes }], nextCursor: '' } });
  expect(read({}).entries[0]?.activity).toBe('held');
  expect(read({ activity: 'consumed', balancePart: 'held' }).entries[0]?.balancePart).toBe('held');
  for (const change of [{ activity: 'consumed' }, { balancePart: 'held', activity: 'posted' }, { activity: 'held', direction: 'in' }, { activity: 'released', direction: 'out' }]) expect(() => read(change)).toThrow();
});
