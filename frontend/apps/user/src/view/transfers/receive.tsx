import { useEffect, useState } from 'react';
import { Button, Tabs } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { ReceiveCodeData } from '@dexwallet/api-contract/platform';
import { getReceiveCode } from '../../api/transfers';
import { Failure, useAuth } from '../../auth/context';
import './style.less';
export default function Receive() {
  const { t } = useTranslation(); const { launch, session } = useAuth();
  const [data, setData] = useState<ReceiveCodeData>(); const [image, setImage] = useState('');
  const [error, setError] = useState<unknown>(); const [copied, setCopied] = useState(false); const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); let active = true;
    setError(undefined); setData(undefined); setImage('');
    void getReceiveCode(controller.signal).then(async result => {
      const { toDataURL } = await import('qrcode');
      const link = launch.mode === 'telegram' && result.telegramUrl ? result.telegramUrl : result.url;
      const png = await toDataURL(link, { errorCorrectionLevel: 'M', width: 320, margin: 4 });
      if (active) { setData(result); setImage(png); }
    }).catch(e => { if (active) setError(e); });
    return () => { active = false; controller.abort(); };
  }, [session.generation, revision, launch.mode]);
  const link = data && (launch.mode === 'telegram' && data.telegramUrl ? data.telegramUrl : data.url);
  return <section className="transfer-page"><h1>{t('receive')}</h1>
    {image && <img className="receive-qr" src={image} alt={t('receiveQR')} />}
    {data && <><p>{data.user.nickname || t('defaultNickname')}</p><p className="transfer-idx">{data.user.idx}</p></>}
    <Tabs><Tabs.Tab title={t('receiveBalance')} key="balance"><p>{t('personalCodeNote')}</p>
      {link && <><label>{t('receiveLink')}<input readOnly value={link} onFocus={e => e.target.select()} /></label><Button onClick={() => { void navigator.clipboard.writeText(link).then(() => setCopied(true), setError); }}>{t(copied ? 'copied' : 'copyLink')}</Button></>}
    </Tabs.Tab><Tabs.Tab title={t('receiveChain')} key="chain"><p>{t(launch.mode === 'telegram' ? 'chainRequiresApp' : 'chainPending')}</p></Tabs.Tab></Tabs>
    {error ? <><Failure error={error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></> : !data && <p role="status">{t('common:loading')}</p>}
  </section>;
}
