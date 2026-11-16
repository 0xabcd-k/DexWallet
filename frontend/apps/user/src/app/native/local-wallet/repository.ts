import { isRecord } from '@dexwallet/api-client';
import { deriveAccounts, parseMaterial, WalletError, walletErrorCode, type WalletAccount, type WalletMaterial, type MnemonicScheme, type TonVersion } from './crypto';

export interface WalletRecord {
  walletId: string; name: string; createdAt: string; state: 'pending' | 'ready'; backedUp: boolean;
  scheme: MnemonicScheme; tonVersion: TonVersion; accounts: WalletAccount[];
}
export interface VaultIdentity { userIdx: string; walletId: string }
export interface VaultRequest extends VaultIdentity { viewToken: string }
export interface VaultPort {
  inspect(value: VaultIdentity): Promise<{ exists: boolean; deviceSecure: boolean }>;
  put(value: VaultRequest & { secret: string }): Promise<void>;
  get(value: VaultRequest): Promise<{ secret: string }>;
  remove(value: VaultRequest): Promise<void>;
}
export interface CatalogPort {
  get(value: { key: string }): Promise<{ value: string | null }>;
  set(value: { key: string; value: string }): Promise<void>;
}
const networks = new Set(['ton-mainnet', 'ton-testnet', 'base', 'solana', 'tron']);
function validRecord(value: unknown): value is WalletRecord {
  if (!isRecord(value) || typeof value.walletId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(value.walletId) ||
    typeof value.name !== 'string' || value.name.length < 1 || value.name.length > 40 || typeof value.createdAt !== 'string' ||
    !Number.isFinite(Date.parse(value.createdAt)) || !['pending', 'ready'].includes(String(value.state)) ||
    typeof value.backedUp !== 'boolean' || !['bip39', 'ton'].includes(String(value.scheme)) || !['v5r1', 'v4r2'].includes(String(value.tonVersion)) ||
    !Array.isArray(value.accounts) || value.accounts.length === 0) return false;
  return value.accounts.every(a => isRecord(a) && typeof a.network === 'string' && networks.has(a.network) &&
    typeof a.address === 'string' && a.address.length > 0 && a.address.length <= 128 && typeof a.path === 'string');
}
function parseCatalog(value: string | null): WalletRecord[] {
  if (value === null) return [];
  let data: unknown;
  try { data = JSON.parse(value); } catch { throw new WalletError('LOCAL_CATALOG_CORRUPTED'); }
  if (!isRecord(data) || data.version !== 1 || !Array.isArray(data.items) || !data.items.every(validRecord) ||
    new Set(data.items.map(r => r.walletId)).size !== data.items.length) throw new WalletError('LOCAL_CATALOG_CORRUPTED');
  return data.items;
}
// Metadata writes from a previous page can finish after remount. Serialize the
// short read/modify/commit section, never the system authentication dialog.
let catalogWrites: Promise<unknown> = Promise.resolve();
// Public metadata and existence only; this does not unlock a key or open a protected view.
export async function listWalletRecords(userIdx: string, catalog: CatalogPort, vault: Pick<VaultPort, 'inspect'>) {
  await catalogWrites;
  const items = parseCatalog((await catalog.get({ key: `dexwallet:wallet-catalog:v1:${userIdx}` })).value);
  return Promise.all(items.map(async record => ({ ...record, available: (await vault.inspect({ userIdx, walletId: record.walletId })).exists })));
}
const beforeWriteFailures = new Set(['VAULT_CANCELLED', 'VAULT_BUSY', 'VAULT_DEVICE_LOCK_REQUIRED', 'VAULT_PRIVACY_REQUIRED', 'VAULT_INVALID_INPUT']);
export function createWalletRepository(userIdx: string, viewToken: string, catalog: CatalogPort, vault: VaultPort) {
  const key = `dexwallet:wallet-catalog:v1:${userIdx}`;
  const identity = (walletId: string): VaultRequest => ({ userIdx, walletId, viewToken });
  const read = async () => parseCatalog((await catalog.get({ key })).value);
  function edit(change: (items: WalletRecord[]) => WalletRecord[]) {
    const next = catalogWrites.then(async () => {
      const items = change(await read());
      await catalog.set({ key, value: JSON.stringify({ version: 1, items }) });
      return items;
    });
    catalogWrites = next.catch(() => {});
    return next;
  }
  async function update(walletId: string, change: (record: WalletRecord) => WalletRecord) {
    await edit(items => {
      if (!items.some(r => r.walletId === walletId)) throw new WalletError('LOCAL_RECORD_MISSING');
      return items.map(r => r.walletId === walletId ? change(r) : r);
    });
  }
  return {
    async list() {
      return listWalletRecords(userIdx, catalog, vault);
    },
    async save(name: string, material: WalletMaterial, accounts: WalletAccount[]) {
      name = name.trim();
      if (!name || name.length > 40) throw new WalletError('LOCAL_NAME_REQUIRED');
      const walletId = crypto.randomUUID();
      if (!(await vault.inspect(identity(walletId))).deviceSecure) throw new WalletError('VAULT_DEVICE_LOCK_REQUIRED');
      const record: WalletRecord = { walletId, name, createdAt: new Date().toISOString(), state: 'pending', backedUp: false,
        scheme: material.scheme, tonVersion: material.tonVersion, accounts };
      await edit(items => {
        if (items.some(r => r.scheme === material.scheme && r.tonVersion === material.tonVersion &&
          JSON.stringify(r.accounts) === JSON.stringify(accounts))) throw new WalletError('LOCAL_DUPLICATE');
        return [...items, record];
      });
      try { await vault.put({ ...identity(walletId), secret: JSON.stringify(material) }); }
      catch (error) {
        if (beforeWriteFailures.has(walletErrorCode(error))) await edit(items => items.filter(r => r.walletId !== walletId));
        throw error;
      }
      await update(walletId, r => ({ ...r, state: 'ready' }));
      return walletId;
    },
    async unlock(record: WalletRecord, onRead?: () => void) {
      const response = await vault.get(identity(record.walletId));
      onRead?.();
      const material = await parseMaterial(response.secret);
      const accounts = await deriveAccounts(material);
      // The authenticated encrypted payload is authoritative, including when a
      // completed native write lost its response before catalog finalization.
      await update(record.walletId, r => ({ ...r, state: 'ready', scheme: material.scheme, tonVersion: material.tonVersion, accounts }));
      return { material, accounts };
    },
    async confirmBackup(record: WalletRecord) { await update(record.walletId, r => ({ ...r, backedUp: true })); },
    async remove(record: WalletRecord) {
      await vault.remove(identity(record.walletId));
      await edit(items => items.filter(r => r.walletId !== record.walletId));
    },
  };
}
export type WalletRepository = ReturnType<typeof createWalletRepository>;
export type ListedWallet = Awaited<ReturnType<WalletRepository['list']>>[number];
