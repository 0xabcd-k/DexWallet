import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router';
import { Button, Input } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { ChainNetwork, ChainAssetBalance } from '@dexwallet/api-contract/platform';
import { Failure, RequireAuth, useAuth } from '../../auth/context';
import { getCatalog, getQuantities } from '../../app/native/chain-assets/api';
import { addressFor, favoritesStore, localWallets, type ViewingWallet } from '../../app/native/chain-assets/wallets';
import { listWalletRecords } from '../../app/native/local-wallet/repository';
import { nativeCatalog, nativeVault } from '../../app/native/local-wallet/bridge';
import { createTonConnection } from '../../app/native/ton';
import './style.less';

export default function ChainAssets() {
  const auth = useAuth();
  return <RequireAuth><WalletAssets key={auth.session.credential?.idx} userIdx={auth.session.credential?.idx ?? ''}/></RequireAuth>;
}
function WalletAssets({ userIdx }: { userIdx: string }) {
  const { t } = useTranslation();
  const [local, setLocal] = useState<ViewingWallet[]>([]), [external, setExternal] = useState<ViewingWallet[]>([]);
  const [loading, setLoading] = useState(true), [restoring, setRestoring] = useState(false);
  const [error, setError] = useState<unknown>(), [connectionError, setConnectionError] = useState(false);
  const [revision, setRevision] = useState(0);
  const favorites = useMemo(() => favoritesStore(userIdx, nativeCatalog), [userIdx]);
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
  const wallets = useMemo(() => [...local, ...external], [local, external]);
  return <section className="chain-assets"><h1>{t('chainAssets')}</h1>
    <p>{t('chainReadOnly')}</p>
    {(loading || restoring) && <p role="status">{t('common:loading')}</p>}
    {error !== undefined && <Failure error={error}/>}{connectionError && <p role="alert">{t('nativeConnectionFailed')}</p>}
    <AssetBrowser wallets={wallets} favorites={favorites}/>
    {!loading && !restoring && !wallets.length && <p>{t('chainNoWallet')}</p>}
    <p><Link to="/local-wallets">{t('localWallets')}</Link>{' · '}<Link to="/wallet-connections">{t('nativeWallets')}</Link></p>
    <Button onClick={() => setRevision(n => n + 1)}>{t('localRefresh')}</Button>
  </section>;
}
interface BrowserProps {
  wallets: ViewingWallet[];
  favorites: ReturnType<typeof favoritesStore>;
  catalog?: typeof getCatalog;
  quantities?: typeof getQuantities;
}
export function AssetBrowser({ wallets, favorites, catalog = getCatalog, quantities = getQuantities }: BrowserProps) {
  const { t } = useTranslation();
  const [networks, setNetworks] = useState<ChainNetwork[]>([]), [catalogError, setCatalogError] = useState<unknown>();
  const [catalogRevision, setCatalogRevision] = useState(0), [catalogLoading, setCatalogLoading] = useState(true);
  const [walletID, setWalletID] = useState(''), [networkID, setNetworkID] = useState(''), [search, setSearch] = useState('');
  const [stars, setStars] = useState<string[]>([]), [favoritesReady, setFavoritesReady] = useState(false), [saving, setSaving] = useState(false), [favoriteError, setFavoriteError] = useState(false);
  const wallet = wallets.find(w => w.id === walletID) ?? wallets[0];
  const compatible = wallet ? networks.filter(n => addressFor(wallet, n)) : [];
  const network = compatible.find(n => n.id === networkID) ?? compatible[0];
  const address = wallet && network ? addressFor(wallet, network) : undefined;
  useEffect(() => {
    const controller = new AbortController(); let active = true; setCatalogError(undefined); setCatalogLoading(true);
    void catalog(controller.signal).then(n => { if (active) setNetworks(n); }, e => { if (active) setCatalogError(e); }).finally(() => { if (active) setCatalogLoading(false); });
    return () => { active = false; controller.abort(); };
  }, [catalog, catalogRevision]);
  useEffect(() => {
    let active = true; setFavoritesReady(false); setStars([]); setFavoriteError(false);
    void favorites.read().then(ids => { if (active) { setStars(ids); setFavoritesReady(true); } }, () => { if (active) setFavoriteError(true); });
    return () => { active = false; };
  }, [favorites]);
  async function toggle(id: string) {
    setSaving(true); setFavoriteError(false); const next = stars.includes(id) ? stars.filter(a => a !== id) : [...stars, id];
    try { await favorites.write(next); setStars(next); } catch { setFavoriteError(true); } finally { setSaving(false); }
  }
  return <>
    {catalogLoading && <p role="status">{t('common:loading')}</p>}
    {catalogError !== undefined && <><Failure error={catalogError}/><Button onClick={() => setCatalogRevision(n => n + 1)}>{t('common:retry')}</Button></>}
    {!!wallets.length && <><label htmlFor="asset-wallet">{t('chainWallet')}</label><select id="asset-wallet" value={wallet?.id ?? ''} onChange={e => { setWalletID(e.target.value); setSearch(''); }}>
      {wallets.map(w => <option key={w.id} value={w.id}>{w.name} · {t(w.source === 'local' ? 'chainLocal' : 'chainExternal')}</option>)}
    </select></>}
    {network && address && <>
      <label htmlFor="asset-network">{t('nativeNetwork')}</label><select id="asset-network" value={network.id} onChange={e => { setNetworkID(e.target.value); setSearch(''); }}>{compatible.map(n => <option key={n.id} value={n.id}>{t(`chainNetwork_${n.id}`)}</option>)}</select>
      {network.testnet && <p className="chain-testnet" role="status">{t('chainTestnet')}</p>}
      <p className="chain-address"><strong>{t('nativeAddress')}</strong><br/>{address}</p>
      <label htmlFor="asset-search">{t('chainSearch')}</label><Input id="asset-search" value={search} onChange={setSearch}/>
      {favoriteError && <p role="alert">{t('chainFavoriteFailed')}</p>}
      <NetworkAssets key={`${wallet?.id}:${network.id}:${address}:${network.enabled}`} network={network} address={address} quantities={quantities} search={search} stars={stars} favoriteDisabled={!favoritesReady || saving} toggle={toggle}/>
    </>}
  </>;
}
function NetworkAssets({ network, address, quantities, search, stars, favoriteDisabled, toggle }: {
  network: ChainNetwork; address: string; quantities: typeof getQuantities; search: string; stars: string[]; favoriteDisabled: boolean; toggle: (id: string) => Promise<void>;
}) {
  const { t, i18n } = useTranslation();
  const [balances, setBalances] = useState<ChainAssetBalance[]>([]), [error, setError] = useState<unknown>(), [busy, setBusy] = useState(network.enabled), [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!network.enabled) return;
    let active = true; const controller = new AbortController(); setBusy(true); setBalances([]); setError(undefined);
    void quantities(network, address, controller.signal).then(b => { if (active) setBalances(b); }, e => { if (active) setError(e); }).finally(() => { if (active) setBusy(false); });
    return () => { active = false; controller.abort(); };
  }, [network, address, quantities, revision]);
  const needle = search.trim().toLowerCase();
  const assets = network.assets.filter(a => `${a.symbol} ${a.contract} ${network.id} ${t(`chainNetwork_${network.id}`)}`.toLowerCase().includes(needle)).sort((a, b) => Number(stars.includes(b.id)) - Number(stars.includes(a.id)));
  return <>
    {!network.enabled && <p role="status">{t('chainDisabled')}</p>}
    {error !== undefined && <Failure error={error}/>}
    {busy && <p role="status">{t('chainLoading')}</p>}
    <Button disabled={busy || !network.enabled} onClick={() => setRevision(n => n + 1)}>{t('chainRefresh')}</Button>
    <p>{t('chainQuantityHint')}</p>
    {!assets.length && <p>{t('chainNoAssets')}</p>}
    <ul className="chain-list">{assets.map(a => {
      const balance = balances.find(b => b.assetId === a.id);
      return <li key={a.id} className="task-card" data-testid={`asset-${a.id}`}><div className="chain-row"><h2>{a.symbol}</h2><Button size="small" aria-pressed={stars.includes(a.id)} disabled={favoriteDisabled} onClick={() => { void toggle(a.id); }}>{t(stars.includes(a.id) ? 'chainUnfavorite' : 'chainFavorite', { symbol: a.symbol })}</Button></div>
        <p className="chain-quantity">{balance?.status === 'ok' ? balance.quantity : busy ? '…' : t('chainUnavailable')}</p>
        {balance?.errorCode === 'NETWORK_MISMATCH' && <p role="alert">{t('chainNetworkMismatch')}</p>}
        {a.origin === 'test_fixture' && <p>{t('chainFixture')}</p>}
        <details><summary>{t('chainDetails')}</summary><dl><dt>{t('nativeNetwork')}</dt><dd>{t(`chainNetwork_${network.id}`)}</dd><dt>{t('chainIdentity')}</dt><dd>{a.contract || t('chainNative')}</dd><dt>{t('chainDecimals')}</dt><dd>{a.decimals}</dd><dt>{t('chainUpdated')}</dt><dd>{balance ? new Date(balance.queriedAt).toLocaleString(i18n.resolvedLanguage) : '—'}</dd></dl></details>
      </li>;
    })}</ul>
  </>;
}
