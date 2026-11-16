// Navigation hints only. The backend revalidates the network, asset and address.
export const CHAIN_RECEIVE_PREFIX = 'com.dexwallet.app://chain-transfer';
export interface ChainReceiveFields { network: string; assetId: string; address: string }
export function receiveFields(raw: string): ChainReceiveFields | undefined {
  if (raw.length > 2048 || raw.trim() !== raw || !raw.startsWith(`${CHAIN_RECEIVE_PREFIX}?`) || raw.includes('#')) return;
  try {
    // Older Android WebViews parse custom schemes as opaque paths. The exact
    // prefix above fixes the authority; only the query needs URL decoding.
    const q = new URLSearchParams(raw.slice(CHAIN_RECEIVE_PREFIX.length + 1));
    if ([...q.keys()].length !== 4 || ['v','network','asset','address'].some(k => q.getAll(k).length !== 1 || !q.get(k)) || q.get('v') !== '1') return;
    const network = q.get('network')!, assetId = q.get('asset')!, address = q.get('address')!;
    if (!/^[a-z]+-[a-z]+$/.test(network) || assetId.length > 180 || !assetId.startsWith(`${network}:`) || address.length > 128 || /[\s\x00-\x1f]/.test(address)) return;
    return { network, assetId, address };
  } catch { return; }
}
export function isChainInput(raw: string): boolean {
  return raw.startsWith(CHAIN_RECEIVE_PREFIX) || /^(?:0x[0-9a-fA-F]{40}|-?\d:[0-9a-fA-F]{64}|[A-Za-z0-9_-]{48}|[1-9A-HJ-NP-Za-km-z]{32,44})$/.test(raw);
}
export const chainPreparationRoute = (raw: string) => `/chain-transfer?request=${encodeURIComponent(raw)}`;
