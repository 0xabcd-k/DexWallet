import { expect, it } from 'vitest';
import { decodeEntries } from '../../apps/user/src/api/entries';

const entry = { idx: 'entry', voucherIdx: 'voucher', businessIdx: 'business', businessType: 'deposit', action: 'post', kind: 'usd_balance', unit: 'USD', direction: 'in', amount: '9007199254740993.01', balanceAfter: '9007199254740993.01', createdAt: '2026-10-30T08:00:00Z' };
const decode = (row: unknown) => decodeEntries({ data: { entries: [row], nextCursor: '' } });
it('preserves exact statement amounts, direction and cursor without floating point conversion', () => {
  expect(decode(entry).entries[0]).toEqual(entry);
  const points = { ...entry, kind: 'task_points', unit: 'POINT', direction: 'out', amount: '9007199254740993', balanceAfter: '0' };
  expect(decode(points).entries[0]).toEqual(points);
  expect(decodeEntries({ data: { entries: [], nextCursor: '' } })).toEqual({ entries: [], nextCursor: '' });
});
it('rejects malformed financial statements instead of inventing an empty or rounded result', () => {
  for (const change of [{ amount: 10 }, { amount: '0.00' }, { amount: '-1.00' }, { amount: '1e5' }, { amount: '1.001' }, { amount: '1000000000000000000.00' }, { balanceAfter: null }, { balanceAfter: '-1.00' }, { unit: 'POINT' }, { direction: 'wrong' }, { createdAt: 'bad' }, { kind: 'unknown' }]) expect(() => decode({ ...entry, ...change })).toThrow();
  expect(() => decode({ ...entry, kind: 'task_points', unit: 'POINT', amount: '1.5', balanceAfter: '2' })).toThrow();
  expect(() => decodeEntries({ data: {} })).toThrow();
  expect(() => decodeEntries({ data: { entries: [], nextCursor: 12 } })).toThrow();
});
