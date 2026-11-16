import { useEffect, useState, type ReactNode } from 'react';
import { Button } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { ChainNetwork, ChainTargetData } from '@dexwallet/api-contract/platform';
import { Failure, useAuth } from '../../auth/context';
import { useViewingWallets } from '../../app/native/chain-assets/use-wallets';
import { addressFor, type ViewingWallet } from '../../app/native/chain-assets/wallets';
import { resolveChainTarget } from '../../app/native/chain-transfer/api';
import { useChainCatalog, WalletSourceStatus } from './sources';
import './style.less';

export default function ChainReceive({ tabs }: { tabs: ReactNode }) {
  const auth = useAuth(), source = useViewingWallets(auth.session.credential!.idx), catalog = useChainCatalog();
  const { t } = useTranslation();
  return <div className="chain-prepare">
    {catalog.loading && <p role="status">{t('common:loading')}</p>}
    {catalog.error !== undefined && <><Failure error={catalog.error}/><Button onClick={catalog.retry}>{t('common:retry')}</Button></>}
    <ChainReceiveForm wallets={source.wallets} networks={catalog.networks} tabs={tabs}/>
    <WalletSourceStatus source={source} returnTo="/receive?tab=chain"/>
  </div>;
}
export function ChainReceiveForm({ wallets, networks, tabs, resolve = resolveChainTarget, makeQR = qrImage }: {
  wallets: ViewingWallet[]; networks: ChainNetwork[]; tabs: ReactNode; resolve?: typeof resolveChainTarget; makeQR?: (code: string) => Promise<string>;
}) {
  const { t } = useTranslation();
  const [walletID, setWalletID] = useState(''), [networkID, setNetworkID] = useState(''), [assetID, setAssetID] = useState('');
  const [result, setResult] = useState<{ key: string; target: ChainTargetData; image: string }>();
  const [failure, setFailure] = useState<{ key: string; error: unknown }>(), [copyError, setCopyError] = useState<unknown>(), [copied, setCopied] = useState('');
  const [revision, setRevision] = useState(0);
  const wallet = wallets.find(w => w.id === walletID) ?? wallets[0];
  const compatible = wallet ? networks.filter(n => addressFor(wallet, n)) : [];
  const network = compatible.find(n => n.id === networkID) ?? compatible[0];
  const asset = network?.assets.find(a => a.id === assetID) ?? network?.assets[0];
  const address = wallet && network ? addressFor(wallet, network) : undefined;
  const key = `${wallet?.id}:${network?.id}:${asset?.id}:${address}:${revision}`;
  const current = result?.key === key ? result : undefined;
  useEffect(() => {
    setCopied(''); setCopyError(undefined);
    if (!network || !asset || !address) return;
    let active = true; const controller = new AbortController();
    void resolve({ network: network.id, assetId: asset.id, address }, networks, controller.signal).then(async target => {
      const image = await makeQR(target.receiveCode); if (active) setResult({ key, target, image });
    }).catch(error => { if (active) setFailure({ key, error }); });
    return () => { active = false; controller.abort(); };
  }, [key, network, asset, address, networks, resolve, makeQR]);
  async function copy(value: string, kind: string) { try { await navigator.clipboard.writeText(value); setCopied(kind); setCopyError(undefined); } catch (e) { setCopyError(e); } }
  return <>
    {current ? <img className="receive-qr" src={current.image} alt={t('chainReceiveQR')}/> : network && failure?.key !== key && <p role="status">{t('common:loading')}</p>}
    {tabs}
    <p>{t('chainReceiveNote')}</p>
    {!!wallets.length && <><label htmlFor="chain-receive-wallet">{t('chainReceiveWallet')}</label><select id="chain-receive-wallet" value={wallet?.id ?? ''} onChange={e => setWalletID(e.target.value)}>{wallets.map(w => <option key={w.id} value={w.id}>{w.name} · {t(w.source === 'local' ? 'chainLocal' : 'chainExternal')}</option>)}</select></>}
    {network && <><label htmlFor="chain-receive-network">{t('nativeNetwork')}</label><select id="chain-receive-network" value={network.id} onChange={e => { setNetworkID(e.target.value); setAssetID(''); }}>{compatible.map(n => <option key={n.id} value={n.id}>{t(`chainNetwork_${n.id}`)}</option>)}</select>
      {network.testnet && <p className="chain-testnet">{t('chainTestnet')}</p>}
      <label htmlFor="chain-receive-asset">{t('chainToken')}</label><select id="chain-receive-asset" value={asset?.id ?? ''} onChange={e => setAssetID(e.target.value)}>{network.assets.map(a => <option key={a.id} value={a.id}>{a.symbol}{a.origin === 'test_fixture' ? ` · ${t('chainFixture')}` : ''}</option>)}</select>
    </>}
    {current && <>
      <p className="chain-address"><strong>{t('nativeAddress')}</strong><br/>{current.target.address}</p>
      {asset?.contract && <p className="chain-address">{t('chainIdentity')}: {asset.contract}</p>}
      <Button onClick={() => { void copy(current.target.address, 'address'); }}>{t(copied === 'address' ? 'copied' : 'chainCopyAddress')}</Button>
      <label htmlFor="chain-receive-code">{t('chainAppCode')}</label><textarea id="chain-receive-code" readOnly value={current.target.receiveCode}/>
      <Button onClick={() => { void copy(current.target.receiveCode, 'code'); }}>{t(copied === 'code' ? 'copied' : 'copyLink')}</Button>
      <p>{t('chainCodeCompatibility')}</p>
    </>}
    {failure?.key === key && <><Failure error={failure.error}/><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
    {copyError !== undefined && <Failure error={copyError}/>}
  </>;
}
// Dense multi-chain links need enough pixels for high-DPI screens and scanners.
async function qrImage(code: string) { return (await import('qrcode')).toDataURL(code, { errorCorrectionLevel: 'M', width: 960, margin: 4 }); }
