import { expect, it } from 'vitest';
import { decodeBalances } from '../../apps/user/src/api/balances';

function accounts() {
  return [
    { kind: 'usd_balance', unit: 'USD', balance: '9007199254740993.01', heldBalance: '2.09', totalBalance: '9007199254740995.10' },
    { kind: 'reward_usd', unit: 'USD', balance: '0.00', heldBalance: '0.00', totalBalance: '0.00' },
    { kind: 'task_points', unit: 'POINT', balance: '9007199254740993', heldBalance: '0', totalBalance: '9007199254740993' },
  ];
}

it('preserves precise strings and allows a future account kind in the same list', () => {
  const rows = [...accounts(), { kind: 'future', unit: 'TOKEN', balance: '0.123456789012345678', heldBalance: '0', totalBalance: '0.123456789012345678' }];
  expect(decodeBalances({ data: { accounts: rows } })).toEqual(rows);
});

it('rejects malformed amounts, mismatched units and incomplete summaries instead of showing zero', () => {
  for (const change of [
    { balance: 12.34 }, { balance: '-1.00' }, { balance: '1e5' }, { balance: '1.001' },
    { heldBalance: null }, { totalBalance: '' }, { unit: 'POINT' }, { kind: '' },
  ]) {
    const rows = accounts(); Object.assign(rows[0]!, change);
    expect(() => decodeBalances({ data: { accounts: rows } })).toThrow();
  }
  const points = accounts(); points[2]!.balance = '1.1';
  expect(() => decodeBalances({ data: { accounts: points } })).toThrow();
  expect(() => decodeBalances({ data: { accounts: [] } })).toThrow();
  expect(() => decodeBalances({ data: { accounts: [...accounts(), accounts()[0]] } })).toThrow();
  expect(() => decodeBalances({ data: {} })).toThrow();
});
