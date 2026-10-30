import { useEffect, useRef, useState } from 'react';
import { ApiError } from '@dexwallet/api-client';
import { Button } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import { authenticate } from '../../api/account';
import { Failure, useAuth } from '../../auth/context';
import { session } from '../../auth/session';

const developmentTelegram = import.meta.env.DEV && import.meta.env.DEV_TELEGRAM;
export default function Telegram({ purpose, automatic = false, onSuccess }: { purpose?: 'bind' | 'replace'; automatic?: boolean; onSuccess: () => void }) {
  const { launch } = useAuth(); const { t } = useTranslation();
  const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>();
  const pending = useRef<Promise<void> | undefined>(undefined); const active = useRef(true);
  const success = useRef(onSuccess); success.current = onSuccess;
  async function run() {
    const ticket = purpose ? session.capture() : await session.begin(session.capture());
    if (!ticket) throw new ApiError('cancelled');
    const material = { initData: launch.rawInitData };
    return authenticate('telegram', purpose ? { ...material, purpose } : material, ticket);
  }
  async function submit(operation?: Promise<void>) {
    setBusy(true); setError(undefined);
    try { await (operation ?? run()); if (active.current) success.current(); }
    catch (failure) { if (active.current) setError(failure); } finally { if (active.current) setBusy(false); }
  }
  useEffect(() => {
    active.current = true;
    if (automatic && launch.mode === 'telegram') { pending.current ??= run(); void submit(pending.current); }
    return () => { active.current = false; };
  }, []);
  return <div className="auth-form"><Button block loading={busy} disabled={busy || launch.mode !== 'telegram'} onClick={() => { void submit(); }}>{t(purpose ? 'useTelegramIdentity' : 'signInTelegram')}</Button>
    {(!developmentTelegram || launch.mode !== 'telegram') && <p className="muted">{t(launch.mode === 'telegram' ? 'telegramFreshProof' : 'telegramOpen')}</p>}
    {error !== undefined && <Failure error={error} />}
  </div>;
}
