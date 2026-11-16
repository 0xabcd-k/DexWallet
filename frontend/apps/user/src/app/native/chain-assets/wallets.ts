import type { ChainNetwork } from '@dexwallet/api-contract/platform';
import type { CatalogPort, WalletRecord } from '../local-wallet/repository';
export interface ViewingWallet { id: string; name: string; source: 'local' | 'external'; accounts: { network: string; address: string }[] }
export function localWallets(records: (WalletRecord & { available: boolean })[]): ViewingWallet[] {
  return records.filter(r => r.state === 'ready' && r.available).map(r => ({ id: r.walletId, name: r.name, source: 'local', accounts: r.accounts }));
}
export function addressFor(wallet: ViewingWallet, network: ChainNetwork) {
  const name = network.family === 'ton' ? network.id : network.family;
  return wallet.accounts.find(a => a.network === name)?.address;
}
export function favoritesStore(userIdx: string, storage: CatalogPort) {
  const key = `dexwallet:asset-favorites:v1:${userIdx}`;
  return {
    async read(): Promise<string[]> {
      const { value } = await storage.get({ key }); if (value === null) return [];
      const data: unknown = JSON.parse(value);
      if (!Array.isArray(data) || !data.every(a => typeof a === 'string' && a.length <= 180 && /^[a-z]+-[a-z]+:[a-z0-9]+(?::[A-Za-z0-9_:-]+)?$/.test(a)) || new Set(data).size !== data.length) throw new TypeError('Invalid favorites');
      return data;
    },
    async write(ids: string[]) { await storage.set({ key, value: JSON.stringify(ids) }); },
  };
}
