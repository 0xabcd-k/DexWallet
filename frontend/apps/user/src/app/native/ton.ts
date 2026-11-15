import TonConnect, { type Wallet, type WalletInfoRemote, isWalletInfoRemote } from '@tonconnect/sdk';
import { tonStorage } from './ton-storage';
import { APP_RETURN_URL } from './lifecycle';
export const SUPPORTED_TON_WALLETS = ['tonkeeper', 'mytonwallet'] as const;
export interface ConnectionState { wallet: Wallet | null; restoring: boolean; connecting: boolean; error?: unknown }
export function createTonConnection(userIdx: string, manifestUrl: string) {
  const manifest = new URL(manifestUrl);
  if (manifest.protocol !== 'https:' || manifest.username || manifest.password) throw new Error('TON manifest requires public HTTPS');
  const storage = tonStorage(userIdx);
  const connector = new TonConnect({ manifestUrl: manifest.href, storage, analytics: { mode: 'off' } });
  let attempt = new AbortController();
  let state: ConnectionState = { wallet: null, restoring: true, connecting: false };
  const listeners = new Set<() => void>();
  let closed = false;
  const emit = (next: ConnectionState) => { if (!closed) { state = next; listeners.forEach(fn => fn()); } };
  const stop = connector.onStatusChange(wallet => emit({ wallet, restoring: false, connecting: false }), error => emit({ ...state, restoring: false, connecting: false, error }));
  const restored = connector.restoreConnection({ openingDeadlineMS: 10000, signal: attempt.signal }).then(() => emit({ wallet: connector.wallet, restoring: false, connecting: false }), error => emit({ ...state, restoring: false, error }));
  async function disconnect() {
    const completed = connector.disconnect();
    // Forget locally before awaiting the bridge, including if the view closes.
    try { await storage.clear(); } finally { await completed; }
    emit({ wallet: null, restoring: false, connecting: false });
  }
  return {
    getSnapshot: () => state,
    subscribe(fn: () => void) { listeners.add(fn); return () => { listeners.delete(fn); }; },
    async wallets(): Promise<WalletInfoRemote[]> {
      return (await connector.getWallets()).filter(isWalletInfoRemote).filter(w => SUPPORTED_TON_WALLETS.some(name => name === w.appName));
    },
    async connect(wallet: WalletInfoRemote) {
      await restored;
      if (closed) throw new Error('Wallet connection view closed');
      if (state.wallet) await disconnect();
      if (closed) throw new Error('Wallet connection view closed');
      attempt = new AbortController();
      const link = connector.connect({ universalLink: wallet.universalLink, bridgeUrl: wallet.bridgeUrl }, undefined, { signal: attempt.signal });
      // Keeper Android 26.10.4 declares the documented tonkeeper:// scheme;
      // the directory's tonkeeper-tc:// scheme is not handled by that APK.
      const url = new URL(wallet.appName === 'tonkeeper' ? 'tonkeeper://ton-connect' : wallet.deepLink || wallet.universalLink);
      url.search = new URL(link).search;
      url.searchParams.set('ret', APP_RETURN_URL);
      emit({ wallet: null, restoring: false, connecting: true });
      return url.href;
    },
    disconnect,
    async cancel() {
      if (connector.wallet) await disconnect();
      attempt.abort();
      await storage.clear();
      emit({ wallet: null, restoring: false, connecting: false });
    },
    dispose() { closed = true; stop(); storage.close(); attempt.abort(); connector.pauseConnection(); listeners.clear(); },
  };
}
export type TonConnection = ReturnType<typeof createTonConnection>;
