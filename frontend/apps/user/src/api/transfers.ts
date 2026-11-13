import { decodeData, isRecord, type Decoder } from '@dexwallet/api-client';
import type { CurrentUserData, ReceiveCodeData, TransferData, TransferPolicyData, TransferTargetRequest, SubmitTransferRequest } from '@dexwallet/api-contract/platform';
import { userRequest } from './account';
import type { Session } from '../auth/session';

export function cents(value: string): bigint | undefined {
  if (!/^(0|[1-9]\d{0,17})(\.\d{1,2})?$/.test(value)) return;
  const [whole, fraction = ''] = value.split('.');
  return BigInt(whole!) * 100n + BigInt(fraction.padEnd(2, '0'));
}
export const decodeTransfer: Decoder<TransferData> = value => {
  if (!isRecord(value) || !isRecord(value.data)) throw new TypeError('Missing transfer');
  const d = value.data;
  if (typeof d.idx !== 'string' || !/^0\d{16,19}$/.test(d.idx) || !isRecord(d.counterparty) ||
      typeof d.counterparty.idx !== 'string' || !/^0\d{16,19}$/.test(d.counterparty.idx) || typeof d.counterparty.nickname !== 'string' || typeof d.counterparty.avatarUrl !== 'string' ||
      typeof d.amount !== 'string' || !/^\d+\.\d{2}$/.test(d.amount) || (cents(d.amount) ?? 0n) <= 0n || d.fee !== '0.00' || d.unit !== 'USD' || d.status !== 'completed' ||
      (d.direction !== 'in' && d.direction !== 'out') || typeof d.createdAt !== 'string' || !Number.isFinite(Date.parse(d.createdAt))) throw new TypeError('Invalid transfer');
  return { idx: d.idx, counterparty: { idx: d.counterparty.idx, nickname: d.counterparty.nickname, avatarUrl: d.counterparty.avatarUrl }, amount: d.amount, fee: d.fee, unit: d.unit, status: d.status, direction: d.direction, createdAt: d.createdAt };
};
export const getReceiveCode = (signal: AbortSignal) => userRequest('/v1/user/receive-code', decodeData<ReceiveCodeData>, { signal });
export const resolveTarget = (body: TransferTargetRequest, ticket: Session) => userRequest('/v1/user/transfer-targets/resolve', decodeData<CurrentUserData>, { method: 'POST', body }, ticket);
export const getTransferPolicy = (ticket: Session) => userRequest('/v1/user/transfer-policy', decodeData<TransferPolicyData>, {}, ticket);
export const submitTransfer = (body: SubmitTransferRequest, ticket: Session) => userRequest('/v1/user/transfers', decodeTransfer, { method: 'POST', body }, ticket);
export const findTransfer = (key: string, ticket: Session) => userRequest(`/v1/user/transfers/by-request/${encodeURIComponent(key)}`, decodeTransfer, {}, ticket);
export const getTransfer = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/transfers/${encodeURIComponent(idx)}`, decodeTransfer, { signal });
