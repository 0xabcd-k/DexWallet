import { useEffect, useMemo, useState } from 'react';
import { listWalletRecords } from '../local-wallet/repository';
import { nativeCatalog, nativeVault } from '../local-wallet/bridge';
import { createTonConnection } from '../ton';
import { localWallets, type ViewingWallet } from './wallets';

// All three read-only wallet screens use the same current metadata/connection source.
export function useViewingWallets(userIdx: string) {
  const [local, setLocal] = useState<ViewingWallet[]>([]), [external, setExternal] = useState<ViewingWallet[]>([]);
  const [loading, setLoading] = useState(true), [restoring, setRestoring] = useState(false);
  const [error, setError] = useState<unknown>(), [connectionError, setConnectionError] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true; setLoading(true); setError(undefined); setLocal([]);
    void listWalletRecords(userIdx, nativeCatalog, nativeVault).then(r => { if (active) setLocal(localWallets(r)); }, e => { if (active) setError(e); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [userIdx, revision]);
  useEffect(() => {
    if (!import.meta.env.TON_MANIFEST_URL) return;
    setRestoring(true); setConnectionError(false); setExternal([]);
    try {
      const connection = createTonConnection(userIdx, import.meta.env.TON_MANIFEST_URL);
      const update = () => {
        const state = connection.getSnapshot(), wallet = state.wallet;
        setRestoring(state.restoring); setConnectionError(!!state.error);
        const network = wallet?.account.chain === '-3' ? 'ton-testnet' : wallet?.account.chain === '-239' ? 'ton-mainnet' : undefined;
        setExternal(wallet && network ? [{ id: `external:${network}:${wallet.account.address}`, name: wallet.device.appName, source: 'external', accounts: [{ network, address: wallet.account.address }] }] : []);
      };
      update(); const stop = connection.subscribe(update);
      return () => { stop(); connection.dispose(); };
    } catch { setRestoring(false); setConnectionError(true); }
  }, [userIdx, revision]);
  return { wallets: useMemo(() => [...local, ...external], [local, external]), loading: loading || restoring, error, connectionError, refresh: () => setRevision(n => n + 1) };
}
