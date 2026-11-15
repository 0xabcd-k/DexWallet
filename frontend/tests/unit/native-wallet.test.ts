import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Wallet, WalletInfoRemote } from '@tonconnect/sdk';
import { tonStorage } from '../../apps/user/src/app/native/ton-storage';
import { nativeDestination, APP_RETURN_URL } from '../../apps/user/src/app/native/lifecycle';
import { safeDestination } from '../../apps/user/src/app/navigation';

const sdk = vi.hoisted(() => ({ instances: [] as FakeConnector[], restore: () => Promise.resolve() }));
interface FakeConnector {
  wallet: Wallet | null;
  status: (wallet: Wallet | null) => void;
  signal?: AbortSignal;
  options: { storage: ReturnType<typeof tonStorage> };
  connect: ReturnType<typeof vi.fn>;
  disconnect: ReturnType<typeof vi.fn>;
}
vi.mock('@tonconnect/sdk', () => ({
  isWalletInfoRemote: (wallet: WalletInfoRemote) => !!wallet.bridgeUrl,
  default: class {
    wallet: Wallet | null = null;
    status = (_wallet: Wallet | null) => {};
    signal?: AbortSignal;
    constructor(public options: FakeConnector['options']) { sdk.instances.push(this); }
    onStatusChange(callback: (wallet: Wallet | null) => void) { this.status = callback; return () => {}; }
    restoreConnection() { return sdk.restore(); }
    getWallets() { return Promise.resolve([{ appName: 'tonkeeper', bridgeUrl: 'https://bridge.example' }, { appName: 'unreviewed', bridgeUrl: 'https://bridge.example' }]); }
    connect = vi.fn((_source: unknown, _request: unknown, options: { signal: AbortSignal }) => { this.signal = options.signal; return 'https://app.tonkeeper.com/ton-connect?v=2&id=test-session&r=connect-only'; });
    disconnect = vi.fn(async () => { this.wallet = null; this.status(null); });
    pauseConnection() {}
  },
}));
import { createTonConnection } from '../../apps/user/src/app/native/ton';
const walletInfo = { appName: 'tonkeeper', name: 'Tonkeeper', universalLink: 'https://app.tonkeeper.com/ton-connect', deepLink: 'tonkeeper-tc://', bridgeUrl: 'https://bridge.example', imageUrl: 'https://example/icon.png', aboutUrl: 'https://tonkeeper.com', platforms: ['android'] } satisfies WalletInfoRemote;
const connected = { account: { address: '0:test-address', chain: '-3', walletStateInit: '', publicKey: '' }, device: { appName: 'tonkeeper', appVersion: 'test', platform: 'android', maxProtocolVersion: 2, features: [] }, provider: 'http' } as Wallet;
beforeEach(() => { localStorage.clear(); sdk.instances.length = 0; sdk.restore = () => Promise.resolve(); });

describe('native return intents', () => {
  it('restores the wallet screen after authentication only in the native build', () => {
    vi.stubEnv('NATIVE_APP', 'true');
    expect(safeDestination('/wallet-connections')).toBe('/wallet-connections');
    vi.stubEnv('NATIVE_APP', '');
    expect(safeDestination('/wallet-connections')).toBe('/wallet');
    vi.unstubAllEnvs();
  });
  it('only navigates the declared callback, rejecting supplied credentials and payment parameters', () => {
    expect(nativeDestination(APP_RETURN_URL)).toBe('/wallet-connections');
    for (const url of ['https://localhost/#/wallet-connections', 'com.dexwallet.app://pay', `${APP_RETURN_URL}?accessToken=forged`, `${APP_RETURN_URL}?amount=100`, `${APP_RETURN_URL}#connected`, 'com.dexwallet.app://attacker@wallet-return', 'garbage']) expect(nativeDestination(url)).toBeUndefined();
  });
});
describe('TON connection storage', () => {
  it('isolates product accounts and preserves saved sessions when a view closes', async () => {
    const data = new Map<string, string>();
    const storage = { async get({key}: {key:string}) { return {value:data.get(key) ?? null}; }, async set({key,value}: {key:string;value:string}) { data.set(key,value); }, async remove({key}: {key:string}) { data.delete(key); }, async keys() { return {keys:[...data.keys()]}; } };
    const a = tonStorage('user-a', storage), b = tonStorage('user-b', storage);
    await a.setItem('session', 'a'); await b.setItem('session', 'b');
    a.close(); await a.setItem('session', 'late-other-state');
    expect(await a.getItem('session')).toBe('a');
    await a.clear(); expect(await b.getItem('session')).toBe('b');
  });
  it('does not silently claim persistence after storage failure', async () => {
    const store = tonStorage('user-a', { async set() { throw new Error('storage denied'); } } as unknown as import('../../apps/user/src/app/native/ton-storage').ConnectionStorage);
    await expect(store.setItem('session', 'value')).rejects.toThrow('storage denied');
  });
});
describe('TON external wallet lifecycle', () => {
  it('restores without prompting a new connection and filters the supported wallet list', async () => {
    const connection = createTonConnection('user-a', 'https://example/manifest.json');
    await Promise.resolve();
    expect(sdk.instances[0]!.connect).not.toHaveBeenCalled();
    expect((await connection.wallets()).map(w => w.appName)).toEqual(['tonkeeper']);
    expect(connection.getSnapshot().restoring).toBe(false);
    connection.dispose();
  });
  it('cancels a pending request without calling disconnect on a non-connected wallet', async () => {
    const connection = createTonConnection('user-a', 'https://example/manifest.json');
    const url = new URL(await connection.connect(walletInfo));
    expect(url.protocol).toBe('tonkeeper:'); expect(url.searchParams.get('ret')).toBe(APP_RETURN_URL);
    const instance = sdk.instances[0]!;
    await instance.options.storage.setItem('pending', 'test-session');
    await connection.cancel();
    expect(instance.signal?.aborted).toBe(true);
    expect(instance.disconnect).not.toHaveBeenCalled();
    expect(await instance.options.storage.getItem('pending')).toBeNull();
    expect(connection.getSnapshot().connecting).toBe(false);
    connection.dispose();
  });
  it('disconnects the previous wallet before switching', async () => {
    const connection = createTonConnection('user-a', 'https://example/manifest.json');
    await Promise.resolve(); const instance = sdk.instances[0]!;
    instance.wallet = connected; instance.status(connected);
    await connection.connect(walletInfo);
    expect(instance.disconnect).toHaveBeenCalledOnce();
    expect(instance.disconnect.mock.invocationCallOrder[0]).toBeLessThan(instance.connect.mock.invocationCallOrder[0]!);
    connection.dispose();
  });
  it('does not launch or adopt a late result after leaving the account view', async () => {
    let resolve!: () => void;
    sdk.restore = () => new Promise<void>(r => { resolve = r; });
    const connection = createTonConnection('user-a', 'https://example/manifest.json');
    const opening = connection.connect(walletInfo);
    connection.dispose(); resolve();
    await expect(opening).rejects.toThrow('view closed');
    sdk.instances[0]!.status(connected);
    expect(connection.getSnapshot().wallet).toBeNull();
    expect(sdk.instances[0]!.connect).not.toHaveBeenCalled();
  });
});
