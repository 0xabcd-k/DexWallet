import { useEffect, useRef, useState } from 'react';
import { Button } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { IScannerControls } from '@zxing/browser';
export function Scanner({ onScan }: { onScan: (raw: string) => void }) {
  const { t } = useTranslation(); const video = useRef<HTMLVideoElement>(null);
  const callback = useRef(onScan); callback.current = onScan;
  const [error, setError] = useState(false); const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true; let controls: IScannerControls | undefined;
    setError(false);
    void import('@zxing/browser').then(async ({ BrowserQRCodeReader }) => {
      if (!active || !video.current) return;
      const reader = new BrowserQRCodeReader();
      const result = await reader.decodeFromConstraints({ video: { facingMode: { ideal: 'environment' } }, audio: false }, video.current, (code, _error, scanner) => {
        if (active && code) { scanner.stop(); callback.current(code.getText()); }
      });
      if (!active) result.stop(); else controls = result;
    }).catch(() => { if (active) setError(true); });
    return () => { active = false; controls?.stop(); };
  }, [revision]);
  return <div className="transfer-scanner"><video ref={video} muted playsInline aria-label={t('scanReceive')} />
    {error && <><p role="alert">{t('cameraUnavailable')}</p><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
  </div>;
}
