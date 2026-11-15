// @vitest-environment node
import { describe, expect, it } from 'vitest';
import { createMaterial, deriveAccounts, importMaterial, parseMaterial } from '../../apps/user/src/app/native/local-wallet/crypto';
import { createWalletRepository } from '../../apps/user/src/app/native/local-wallet/repository';
import { boundaries, mnemonic } from './local-wallet-fixture';

async function fixture() { const material = await importMaterial(mnemonic, 'bip39', 'v5r1'); return { material, accounts: await deriveAccounts(material) }; }
describe('local wallet derivation', () => {
  it('matches independently established first-account addresses and keeps TON networks separate', async () => {
    const { accounts } = await fixture();
    expect(accounts.find(a => a.network === 'base')?.address).toBe('0x9858EfFD232B4033E47d90003D41EC34EcaEda94');
    expect(accounts.find(a => a.network === 'tron')?.address).toBe('TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH');
    expect(accounts.find(a => a.network === 'solana')?.address).toBe('HAgk14JpMQLgt6rVgv7cBQFJWFto5Dqxi472uT3DKpqk');
    expect(accounts[0]?.address).toBe('UQBHyu-oZVDHRYQ1-rKlGqpHy5yAqanPBirEQNMNOmfHLtaT');
    expect(accounts[1]?.address).toBe('0QCf9EYCGEnzNflKo0BRm4IM0g2i094C_uqgJBSBmHZMUG87');
    expect(accounts).toHaveLength(5);
  });
  it('creates 24 words and restores exactly the same accounts after normalization', async () => {
    const created = createMaterial();
    expect(created.words.split(' ')).toHaveLength(24);
    const restored = await importMaterial('  ' + created.words.toUpperCase().replaceAll(' ', '\n') + '  ', 'bip39', 'v5r1');
    expect(await deriveAccounts(restored)).toEqual(await deriveAccounts(created));
    expect(await parseMaterial(JSON.stringify(created))).toEqual(created);
  });
  it('rejects invalid checksums, unsupported formats and unknown stored versions', async () => {
    await expect(importMaterial('abandon '.repeat(12), 'bip39', 'v5r1')).rejects.toMatchObject({ code: 'LOCAL_INVALID_MNEMONIC' });
    await expect(importMaterial(mnemonic, 'ton', 'v5r1')).rejects.toMatchObject({ code: 'LOCAL_INVALID_MNEMONIC' });
    await expect(parseMaterial(JSON.stringify({ version: 2, words: mnemonic }))).rejects.toMatchObject({ code: 'LOCAL_INVALID_SECRET' });
  });
  it('keeps the selected TON contract version in recovery data without changing other chain accounts', async () => {
    const { accounts } = await fixture();
    const v4 = await deriveAccounts(await importMaterial(mnemonic, 'bip39', 'v4r2'));
    expect(v4[0]?.address).not.toBe(accounts[0]?.address);
    expect(v4.slice(2)).toEqual(accounts.slice(2));
  });
  it('restores the public TON-native vector using its explicit scheme and contract version', async () => {
    // TON Crypto MIT-licensed vector #0: https://github.com/ton-org/ton-crypto/blob/master/src/mnemonic/mnemonic.spec.ts
    // Public fixture only; never fund it. Expected addresses use the vector's published public key.
    const words = 'hospital stove relief fringe tongue always charge angry urge sentence again match nerve inquiry senior coconut label tumble carry category beauty bean road solution';
    const v5 = await importMaterial(words, 'ton', 'v5r1');
    const accounts = await deriveAccounts(v5);
    expect(accounts).toHaveLength(2);
    expect(accounts[0]?.address).toBe('UQDSLOFVamNZzdy4LulclcCBEFkRReZ7WscBCLAw3Pg53h3h');
    expect((await deriveAccounts(await parseMaterial(JSON.stringify(v5))))).toEqual(accounts);
    expect((await deriveAccounts(await importMaterial(words, 'ton', 'v4r2')))[0]?.address).toBe('UQAYZ-rchbaHK4IUkCvcyl3V8spcAnO8IBRCNvS3are-CbyQ');
    await expect(importMaterial(words, 'bip39', 'v5r1')).rejects.toMatchObject({ code: 'LOCAL_INVALID_MNEMONIC' });
  });
});
describe('local wallet persistence and recovery', () => {
  it('stores only public metadata in the catalog and isolates product users', async () => {
    const f = boundaries(), { material, accounts } = await fixture();
    const a = createWalletRepository('a', 'view', f.catalog, f.vault), b = createWalletRepository('b', 'view', f.catalog, f.vault);
    await a.save('My wallet', material, accounts);
    expect(await b.list()).toEqual([]);
    expect((await a.list())[0]).toMatchObject({ state: 'ready', backedUp: false, available: true });
    expect([...f.metadata.values()].join()).not.toContain(mnemonic);
    await expect(a.save('Duplicate', material, accounts)).rejects.toMatchObject({ code: 'LOCAL_DUPLICATE' });
  });
  it('does not write a secret when device protection or catalog persistence is unavailable', async () => {
    const f = boundaries(), { material, accounts } = await fixture(), repo = createWalletRepository('a', 'view', f.catalog, f.vault);
    f.control.secure = false;
    await expect(repo.save('Wallet', material, accounts)).rejects.toMatchObject({ code: 'VAULT_DEVICE_LOCK_REQUIRED' });
    f.control.secure = true; f.control.failCatalog = true;
    await expect(repo.save('Wallet', material, accounts)).rejects.toThrow('disk unavailable');
    expect(f.control.puts).toBe(0); expect(f.secrets.size).toBe(0);
  });
  it('removes a cancelled attempt without creating a wallet', async () => {
    const f = boundaries(), { material, accounts } = await fixture(), repo = createWalletRepository('a', 'view', f.catalog, f.vault);
    f.control.rejectPut = 'VAULT_CANCELLED';
    await expect(repo.save('Wallet', material, accounts)).rejects.toMatchObject({ code: 'VAULT_CANCELLED' });
    expect(await repo.list()).toEqual([]); expect(f.secrets.size).toBe(0);
  });
  it.each(['native reply', 'catalog finalization'])('recovers the original saved wallet after losing %s', async failure => {
    const f = boundaries(), { material, accounts } = await fixture(), repo = createWalletRepository('a', 'view', f.catalog, f.vault);
    f.control.losePutReply = failure === 'native reply'; f.control.failReady = failure === 'catalog finalization';
    await expect(repo.save('Wallet', material, accounts)).rejects.toThrow();
    const original = (await repo.list())[0]!;
    expect(original).toMatchObject({ state: 'pending', available: true });
    f.control.losePutReply = false; f.control.failReady = false;
    const reopened = createWalletRepository('a', 'next-view', f.catalog, f.vault);
    expect((await reopened.unlock(original)).accounts).toEqual(accounts);
    expect((await reopened.list())[0]).toMatchObject({ walletId: original.walletId, state: 'ready' });
    expect(f.control.puts).toBe(1);
  });
  it('retains a record after an unknown removal, then confirms removal using the same wallet id', async () => {
    const f = boundaries(), { material, accounts } = await fixture(), repo = createWalletRepository('a', 'view', f.catalog, f.vault);
    await repo.save('Wallet', material, accounts); const record = (await repo.list())[0]!;
    f.control.loseRemoveReply = true;
    await expect(repo.remove(record)).rejects.toThrow();
    expect((await repo.list())[0]).toMatchObject({ walletId: record.walletId, available: false });
    f.control.loseRemoveReply = false; await repo.remove(record); expect(await repo.list()).toEqual([]);
    await repo.save('Restored', await importMaterial(material.words, material.scheme, material.tonVersion), accounts);
    expect((await repo.list())[0]?.accounts).toEqual(accounts);
  });
  it('does not silently reset a corrupt catalog or accept a corrupt secret', async () => {
    const f = boundaries(), { material, accounts } = await fixture(), repo = createWalletRepository('a', 'view', f.catalog, f.vault);
    await repo.save('Wallet', material, accounts); const record = (await repo.list())[0]!;
    f.secrets.set(`a:${record.walletId}`, '{bad json');
    await expect(repo.unlock(record)).rejects.toMatchObject({ code: 'LOCAL_INVALID_SECRET' });
    const key = [...f.metadata.keys()][0]!; f.metadata.set(key, '{bad json');
    await expect(repo.list()).rejects.toMatchObject({ code: 'LOCAL_CATALOG_CORRUPTED' });
    expect(f.metadata.get(key)).toBe('{bad json');
  });
});
