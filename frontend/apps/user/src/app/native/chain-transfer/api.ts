import { isRecord } from '@dexwallet/api-client';
import type { ChainNetwork, ChainTargetData, ChainTargetRequest, ChainTransferPreviewData, ChainTransferPreviewRequest } from '@dexwallet/api-contract/platform';
import { userRequest } from '../../../api/account';
import { receiveFields } from '../../chain-intent';
import { canonicalAddress } from '../chain-assets/api';

export function atomicAmount(raw: string, decimals: number): bigint | undefined {
  if (raw.length > 100 || !/^(0|[1-9]\d*)(\.\d+)?$/.test(raw)) return;
  const [whole, fraction = ''] = raw.split('.'); if (fraction.length > decimals) return;
  return BigInt(whole! + fraction.padEnd(decimals, '0'));
}
function targetData(value: unknown, networks: ChainNetwork[], expected: ChainTargetRequest): ChainTargetData {
  if (!isRecord(value) || typeof value.network !== 'string' || typeof value.assetId !== 'string' || typeof value.address !== 'string' || typeof value.receiveCode !== 'string') throw new TypeError('Missing chain target');
  const n = networks.find(n => n.id === value.network), code = receiveFields(value.receiveCode);
  const requested = expected.receiveCode ? receiveFields(expected.receiveCode) : expected;
  if (!n || !n.assets.some(a => a.id === value.assetId) || !code || !requested || code.network !== n.id || code.assetId !== value.assetId || code.address !== value.address ||
    requested.network !== n.id || requested.assetId !== value.assetId || !requested.address || canonicalAddress(n, requested.address) !== canonicalAddress(n, value.address)) throw new TypeError('Mismatched chain target');
  return { network: n.id, assetId: value.assetId, address: value.address, receiveCode: value.receiveCode };
}
export function resolveChainTarget(input: ChainTargetRequest, networks: ChainNetwork[], signal: AbortSignal) {
  return userRequest('/v1/user/chain-transfer-targets/resolve', value => {
    if (!isRecord(value)) throw new TypeError('Invalid target envelope');
    return targetData(value.data, networks, input);
  }, { method: 'POST', body: input, signal });
}
export function decodePreview(value: unknown, networks: ChainNetwork[], input: ChainTransferPreviewRequest): ChainTransferPreviewData {
  if (!isRecord(value) || !isRecord(value.data)) throw new TypeError('Missing chain preview');
  const d = value.data, target = targetData(d.target, networks, input);
  const asset = networks.find(n => n.id === target.network)!.assets.find(a => a.id === target.assetId)!;
  if (typeof d.amount !== 'string' || (atomicAmount(d.amount, asset.decimals) ?? 0n) <= 0n || atomicAmount(d.amount, asset.decimals) !== atomicAmount(input.amount, asset.decimals) ||
    d.canSubmit !== false || d.blockReason !== 'CHANNEL_NOT_OPEN') throw new TypeError('Invalid preparation result');
  return { target, amount: d.amount, canSubmit: false, blockReason: 'CHANNEL_NOT_OPEN' };
}
export const previewChainTransfer = (input: ChainTransferPreviewRequest, networks: ChainNetwork[], signal: AbortSignal) => userRequest('/v1/user/chain-transfers/preview', value => decodePreview(value, networks, input), { method: 'POST', body: input, signal });
