import type { CatalogPort, VaultPort, VaultIdentity, VaultRequest } from '../../apps/user/src/app/native/local-wallet/repository';
import { WalletError } from '../../apps/user/src/app/native/local-wallet/crypto';
// Public BIP39 test vector; never fund these addresses.
export const mnemonic = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
// Public TON Crypto vector #0 from https://github.com/ton-org/ton-crypto/blob/master/src/mnemonic/mnemonic.spec.ts
export const tonMnemonic = 'hospital stove relief fringe tongue always charge angry urge sentence again match nerve inquiry senior coconut label tumble carry category beauty bean road solution';
export function boundaries() {
  const metadata = new Map<string, string>(), secrets = new Map<string, string>();
  const id = (v: VaultIdentity) => `${v.userIdx}:${v.walletId}`;
  const control = { secure: true, rejectPut: '', losePutReply: false, failCatalog: false, failReady: false, loseRemoveReply: false, puts: 0 };
  const catalog: CatalogPort = {
    async get({ key }) { return { value: metadata.get(key) ?? null }; },
    async set({ key, value }) {
      if (control.failCatalog || control.failReady && value.includes('"state":"ready"')) throw new Error('disk unavailable');
      metadata.set(key, value);
    },
  };
  const vault: VaultPort = {
    async inspect(v) { return { exists: secrets.has(id(v)), deviceSecure: control.secure }; },
    async put(v) {
      control.puts++;
      if (control.rejectPut) throw new WalletError(control.rejectPut);
      if (secrets.has(id(v))) throw new WalletError('VAULT_EXISTS');
      secrets.set(id(v), v.secret);
      if (control.losePutReply) throw new Error('response lost');
    },
    async get(v: VaultRequest) {
      const secret = secrets.get(id(v));
      if (!secret) throw new WalletError('VAULT_NOT_FOUND');
      return { secret };
    },
    async remove(v) {
      secrets.delete(id(v));
      if (control.loseRemoveReply) throw new Error('response lost');
    },
  };
  return { metadata, secrets, control, catalog, vault };
}
