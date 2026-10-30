import { isRecord, type Decoder } from '@dexwallet/api-client';
import type { AccountHold, AccountHoldsData, AccountHoldsRequestParams } from '@dexwallet/api-contract/platform';
import { userRequest } from './account';

// Keep aligned with AccountHoldsRequest in platform.api.
export const HOLD_PAGE_SIZE = 20;
const amountPattern = /^(0|[1-9]\d{0,17})\.\d{2}$/;
export const decodeHolds: Decoder<AccountHoldsData> = value => {
  if (!isRecord(value) || !isRecord(value.data) || !Array.isArray(value.data.holds) || typeof value.data.nextCursor !== 'string') throw new TypeError('Missing holds page');
  const holds = value.data.holds.map((item: unknown): AccountHold => {
    if (!isRecord(item)) throw new TypeError('Invalid hold');
    const { idx, businessType, businessIdx, amount, unit, status, reason, holdVoucherIdx, resolvedVoucherIdx, createdAt, resolvedAt } = item;
    if (typeof idx !== 'string' || !idx || (businessType !== 'withdrawal' && businessType !== 'payment') || typeof businessIdx !== 'string' || !businessIdx ||
      typeof amount !== 'string' || !amountPattern.test(amount) || !/[1-9]/.test(amount) || unit !== 'USD' ||
      (status !== 'held' && status !== 'consumed' && status !== 'released') || typeof reason !== 'string' ||
      typeof holdVoucherIdx !== 'string' || !holdVoucherIdx || typeof resolvedVoucherIdx !== 'string' ||
      typeof createdAt !== 'string' || !Number.isFinite(Date.parse(createdAt)) || typeof resolvedAt !== 'string') throw new TypeError('Invalid hold fields');
    if (status === 'held' ? reason !== '' || resolvedVoucherIdx !== '' || resolvedAt !== '' :
      !resolvedVoucherIdx || !Number.isFinite(Date.parse(resolvedAt)) || (status === 'consumed' ? reason !== 'completed' : !['cancelled', 'rejected', 'not_executed'].includes(reason))) throw new TypeError('Invalid hold resolution');
    return { idx, businessType, businessIdx, amount, unit, status, reason, holdVoucherIdx, resolvedVoucherIdx, createdAt, resolvedAt };
  });
  return { holds, nextCursor: value.data.nextCursor };
};
export function getHolds(status: string, before: string, signal: AbortSignal) {
  const request: AccountHoldsRequestParams = { status, before, limit: HOLD_PAGE_SIZE };
  const query = new URLSearchParams({ limit: String(request.limit) });
  if (request.status) query.set('status', request.status);
  if (request.before) query.set('before', request.before);
  return userRequest(`/v1/user/account-holds?${query}`, decodeHolds, { signal });
}
