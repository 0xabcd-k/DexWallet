import '../local-wallet/buffer';
import { Address } from '@ton/core';
import { isRecord, type Decoder } from '@dexwallet/api-client';
import type { ChainAsset, ChainNetwork, ChainAssetBalance } from '@dexwallet/api-contract/platform';
import { userRequest } from '../../../api/account';

const families = ['ton', 'base', 'solana', 'tron'];
export const decodeCatalog: Decoder<ChainNetwork[]> = value => {
  if (!isRecord(value) || !isRecord(value.data) || !Array.isArray(value.data.networks)) throw new TypeError('Missing network catalog');
  const networkIDs = new Set<string>(), assetIDs = new Set<string>();
  return value.data.networks.map((n: unknown): ChainNetwork => {
    if (!isRecord(n) || typeof n.id !== 'string' || !/^[a-z]+-[a-z]+$/.test(n.id) || networkIDs.has(n.id) ||
      typeof n.family !== 'string' || !families.includes(n.family) || typeof n.testnet !== 'boolean' || typeof n.enabled !== 'boolean' || !Array.isArray(n.assets) || !n.assets.length) throw new TypeError('Invalid network');
    networkIDs.add(n.id);
    const assets = n.assets.map((a: unknown): ChainAsset => {
      if (!isRecord(a) || typeof a.id !== 'string' || !a.id.startsWith(`${n.id}:`) || assetIDs.has(a.id) ||
        (a.depositPreview !== undefined && typeof a.depositPreview !== 'boolean') || typeof a.symbol !== 'string' || !/^[A-Z0-9]{1,12}$/.test(a.symbol) || typeof a.contract !== 'string' || a.contract.length > 128 ||
        typeof a.decimals !== 'number' || !Number.isInteger(a.decimals) || a.decimals < 0 || a.decimals > 18 ||
        !['native', 'issuer', 'test_fixture'].includes(String(a.origin)) || (a.origin === 'test_fixture' && !n.testnet) ||
        (a.origin === 'native') !== (a.contract === '')) throw new TypeError('Invalid asset identity');
      assetIDs.add(a.id);
      return a as unknown as ChainAsset;
    });
    return { id: n.id, family: n.family as ChainNetwork['family'], testnet: n.testnet, enabled: n.enabled, assets };
  });
};
export function canonicalAddress(network: ChainNetwork, address: string) {
  return network.family === 'ton' ? Address.parse(address).toRawString() : network.family === 'base' ? address.toLowerCase() : address;
}
export function decodeQuantities(network: ChainNetwork, address: string): Decoder<ChainAssetBalance[]> {
  return value => {
    if (!isRecord(value) || !isRecord(value.data) || value.data.network !== network.id || typeof value.data.address !== 'string' ||
      canonicalAddress(network, value.data.address) !== canonicalAddress(network, address) || !Array.isArray(value.data.items) || value.data.items.length !== network.assets.length) throw new TypeError('Mismatched balance query');
    const seen = new Set<string>();
    return value.data.items.map((b: unknown): ChainAssetBalance => {
      if (!isRecord(b) || typeof b.assetId !== 'string' || seen.has(b.assetId)) throw new TypeError('Duplicate/missing asset');
      const asset = network.assets.find(a => a.id === b.assetId);
      if (!asset || typeof b.quantity !== 'string' || typeof b.queriedAt !== 'number' || !Number.isSafeInteger(b.queriedAt) || b.queriedAt <= 0) throw new TypeError('Invalid balance');
      if (b.status === 'ok') {
        if (b.errorCode !== 'NONE' || b.quantity.length > 100 || !/^(0|[1-9]\d*)(\.\d+)?$/.test(b.quantity) || (b.quantity.split('.')[1]?.length ?? 0) > asset.decimals) throw new TypeError('Invalid quantity');
      } else if (b.status !== 'unavailable' || b.quantity !== '' || !['UPSTREAM_UNAVAILABLE', 'UPSTREAM_INVALID', 'NETWORK_MISMATCH'].includes(String(b.errorCode))) throw new TypeError('Invalid failure');
      seen.add(b.assetId); return b as unknown as ChainAssetBalance;
    });
  };
}
export const getCatalog = (signal: AbortSignal) => userRequest('/v1/user/chain-assets', decodeCatalog, { signal });
export const getQuantities = (network: ChainNetwork, address: string, signal: AbortSignal) => userRequest('/v1/user/chain-assets/balances', decodeQuantities(network, address), { method: 'POST', body: { network: network.id, address }, signal });
