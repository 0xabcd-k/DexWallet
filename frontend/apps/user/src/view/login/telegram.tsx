import { useEffect, useRef, useState } from 'react';
import { ApiError } from '@dexwallet/api-client';
import { Button } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import { authenticate } from '../../api/account';
import { Failure, useAuth } from '../../auth/context';
import { randomRequestToken, session } from '../../auth/session';

const TELEGRAM_ATTEMPT_KEY = 'dexwallet:user:telegram-attempt';
function loginRequest(fingerprint: string): string {
  // Persist only the recovery handle, never an additional copy of initData.
  try {
    const saved: unknown = JSON.parse(sessionStorage.getItem(TELEGRAM_ATTEMPT_KEY) ?? 'null');
    if (typeof saved === 'object' && saved !== null && 'fingerprint' in saved && saved.fingerprint === fingerprint &&
      'requestToken' in saved && typeof saved.requestToken === 'string' && /^[a-f0-9]{64}$/.test(saved.requestToken)) return saved.requestToken;
    const requestToken = randomRequestToken(); sessionStorage.setItem(TELEGRAM_ATTEMPT_KEY, JSON.stringify({ fingerprint, requestToken })); return requestToken;
  } catch { return randomRequestToken(); }
}
export default function Telegram({ purpose, automatic = false, onSuccess }: { purpose?: 'bind' | 'replace'; automatic?: boolean; onSuccess: () => void }) {
  const { launch } = useAuth(); const { t } = useTranslation();
  const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>();
  const [attempt] = useState(() => ({ started: false, ticket: session.capture(), requestToken: purpose ? randomRequestToken() : loginRequest(launch.fingerprint) }));
  const pending = useRef<Promise<void> | undefined>(undefined); const active = useRef(true);
  const success = useRef(onSuccess); success.current = onSuccess;
  async function run() {
    if (!purpose && !attempt.started) {
      const ticket = await session.begin(session.capture());
      if (!ticket) throw new ApiError('cancelled');
      attempt.ticket = ticket; attempt.started = true;
    }
    const material = { requestToken: attempt.requestToken, initData: launch.rawInitData };
    return authenticate('telegram', purpose ? { ...material, purpose } : material, attempt.ticket);
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
    <p className="muted">{t(launch.mode === 'telegram' ? 'telegramFreshProof' : 'telegramOpen')}</p>
    {error !== undefined && <Failure error={error} />}
  </div>;
}
