import { isRecord, type Decoder } from '@dexwallet/api-client';
import type { AccountEntry, AccountEntriesData, AccountEntriesRequestParams } from '@dexwallet/api-contract/platform';
import { AccountKind } from './balances';
import { userRequest } from './account';

// Keep aligned with AccountEntriesRequest's default in platform.api.
export const ENTRY_PAGE_SIZE = 20;
const usdAmount = /^(0|[1-9]\d{0,17})\.\d{2}$/;
const pointsAmount = /^(0|[1-9]\d{0,17})$/;

export const decodeEntries: Decoder<AccountEntriesData> = value => {
  if (!isRecord(value) || !isRecord(value.data) || !Array.isArray(value.data.entries) || typeof value.data.nextCursor !== 'string') throw new TypeError('Missing statement page');
  const entries = value.data.entries.map((item: unknown): AccountEntry => {
    if (!isRecord(item)) throw new TypeError('Invalid entry');
    const { idx, voucherIdx, businessType, businessIdx, action, kind, unit, direction, amount, balanceAfter, balancePart, activity, createdAt } = item;
    if (typeof idx !== 'string' || !idx || typeof voucherIdx !== 'string' || typeof businessType !== 'string' ||
        typeof businessIdx !== 'string' || typeof action !== 'string' || typeof kind !== 'string' || typeof unit !== 'string' ||
        (balancePart !== 'available' && balancePart !== 'held') || (activity !== 'posted' && activity !== 'held' && activity !== 'consumed' && activity !== 'released') || (direction !== 'in' && direction !== 'out') || typeof amount !== 'string' || typeof balanceAfter !== 'string' ||
        typeof createdAt !== 'string' || !Number.isFinite(Date.parse(createdAt))) throw new TypeError('Invalid entry fields');
    const points = kind === AccountKind.points;
    if (!points && kind !== AccountKind.usd && kind !== AccountKind.reward) throw new TypeError('Unknown account kind');
    const pattern = points ? pointsAmount : usdAmount;
    if (unit !== (points ? 'POINT' : 'USD') || !pattern.test(amount) || !/[1-9]/.test(amount) || !pattern.test(balanceAfter)) throw new TypeError('Invalid entry amount or unit');
    if ((balancePart === 'held' && (kind !== AccountKind.usd || activity !== 'consumed')) || (activity === 'consumed' && (balancePart !== 'held' || direction !== 'out')) || (activity === 'held' && direction !== 'out') || (activity === 'released' && direction !== 'in') || (activity !== 'posted' && kind !== AccountKind.usd)) throw new TypeError('Invalid hold activity');
    return { idx, voucherIdx, businessType, businessIdx, action, kind, unit, direction, amount, balanceAfter, balancePart, activity, createdAt };
  });
  return { entries, nextCursor: value.data.nextCursor };
};

export function getEntries(kind: string, before: string, signal: AbortSignal) {
  const request: AccountEntriesRequestParams = { kind, before, limit: ENTRY_PAGE_SIZE };
  const query = new URLSearchParams({ limit: String(request.limit) });
  if (request.kind) query.set('kind', request.kind);
  if (request.before) query.set('before', request.before);
  return userRequest(`/v1/user/account-entries?${query}`, decodeEntries, { signal });
}
