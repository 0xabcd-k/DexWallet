import { isRecord, type Decoder } from '@dexwallet/api-client';
import type { AccountBalance } from '@dexwallet/api-contract/platform';
import { userRequest } from './account';

// These are the account kinds this UI presents. Unknown future kinds can pass
// through the list without being mistaken for one of the existing balances.
export const AccountKind = { usd: 'usd_balance', reward: 'reward_usd', points: 'task_points' } as const;
const expectedUnits: Record<string, string> = {
  [AccountKind.usd]: 'USD', [AccountKind.reward]: 'USD', [AccountKind.points]: 'POINT',
};
const amountPattern = /^(0|[1-9]\d*)(?:\.\d+)?$/;

export const decodeBalances: Decoder<AccountBalance[]> = value => {
  if (!isRecord(value) || !isRecord(value.data) || !Array.isArray(value.data.accounts)) throw new TypeError('Missing accounts');
  const seen = new Set<string>();
  const accounts = value.data.accounts.map((item: unknown): AccountBalance => {
    if (!isRecord(item) || typeof item.kind !== 'string' || !item.kind || seen.has(item.kind) ||
        typeof item.unit !== 'string' || !item.unit.trim()) throw new TypeError('Invalid account kind or unit');
    const { kind, unit, balance, heldBalance, totalBalance } = item;
    if (typeof balance !== 'string' || typeof heldBalance !== 'string' || typeof totalBalance !== 'string') throw new TypeError('Amounts must be strings');
    for (const amount of [balance, heldBalance, totalBalance]) {
      if (amount.length > 64 || !amountPattern.test(amount)) throw new TypeError('Invalid balance');
      if (expectedUnits[kind] === 'USD' && !/\.\d{2}$/.test(amount)) throw new TypeError('Invalid USD representation');
      if (kind === AccountKind.points && amount.includes('.')) throw new TypeError('Invalid points representation');
    }
    if (expectedUnits[kind] && unit !== expectedUnits[kind]) throw new TypeError('Mismatched account unit');
    seen.add(kind);
    return { kind, unit, balance, heldBalance, totalBalance };
  });
  if (Object.values(AccountKind).some(kind => !seen.has(kind))) throw new TypeError('Incomplete account summary');
  return accounts;
};

export const getBalances = (signal: AbortSignal) => userRequest('/v1/user/accounts', decodeBalances, { signal });
