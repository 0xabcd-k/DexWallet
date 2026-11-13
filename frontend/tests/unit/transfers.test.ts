import { describe, expect, it } from 'vitest';
import { cents, decodeTransfer } from '../../apps/user/src/api/transfers';
import { readPending, savePending } from '../../apps/user/src/view/transfers/pending';
describe('payment recovery', () => {
  it('preserves the original key and parameters after reload, and isolates accounts', () => {
    const rows = new Map<string, string>(); const storage = { getItem: (k: string) => rows.get(k) ?? null, setItem: (k: string, v: string) => { rows.set(k, v); } };
    const saved = savePending(storage, 'sender', '01234567890123456', '10.01');
    expect(readPending(storage, 'sender')).toEqual(saved);
    expect(savePending(storage, 'sender', '09876543210987654', '20')).toEqual(saved);
    expect(readPending(storage, 'different-user')).toBeUndefined();
  });
  it('cannot authorize sending if recovery data is lost or damaged', () => {
    expect(() => savePending({ getItem: () => null, setItem: () => {} }, 'sender', '01234567890123456', '10')).toThrow();
    expect(() => readPending({ getItem: () => '{broken' }, 'sender')).toThrow();
  });
  it('compares USD exactly without numeric rounding or coercing extra precision', () => {
    expect(cents('9007199254740993.01')).toBe(900719925474099301n);
    for (const v of ['1e2', '01', '-1', '1.001', ' 1', '.01']) expect(cents(v)).toBeUndefined();
  });
  it('does not turn an accepted or partial response into completed payment', () => {
    expect(() => decodeTransfer({ data: { status: 'accepted', amount: '10' } })).toThrow();
  });
});
