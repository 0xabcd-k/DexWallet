import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { Button } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { ChainNetwork } from '@dexwallet/api-contract/platform';
import { Failure } from '../../auth/context';
import { getCatalog } from '../../app/native/chain-assets/api';
import type { useViewingWallets } from '../../app/native/chain-assets/use-wallets';
export function useChainCatalog() {
  const [networks, setNetworks] = useState<ChainNetwork[]>([]), [loading, setLoading] = useState(true), [error, setError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true; const controller = new AbortController(); setLoading(true); setError(undefined);
    void getCatalog(controller.signal).then(n => { if (active) setNetworks(n); }, e => { if (active) setError(e); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; controller.abort(); };
  }, [revision]);
  return { networks, loading, error, retry: () => setRevision(n => n + 1) };
}
export function WalletSourceStatus({ source, returnTo }: { source: ReturnType<typeof useViewingWallets>; returnTo?: string }) {
  const { t } = useTranslation();
  return <>
    {source.loading && <p role="status">{t('common:loading')}</p>}
    {source.error !== undefined && <Failure error={source.error}/>}
    {source.connectionError && <p role="alert">{t('nativeConnectionFailed')}</p>}
    {!source.loading && !source.wallets.length && <p>{t('chainNoWallet')}</p>}
    <p><Link to="/local-wallets" state={{ from: returnTo }}>{t('localWallets')}</Link>{' · '}<Link to="/wallet-connections" state={{ from: returnTo }}>{t('nativeWallets')}</Link></p>
    <Button onClick={source.refresh}>{t('localRefresh')}</Button>
  </>;
}
