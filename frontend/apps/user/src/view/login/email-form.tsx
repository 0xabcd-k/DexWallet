import { useEffect, useRef, useState } from 'react';
import { Button, Input } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { EmailLoginRequest, IdentityEmailRequest, ChallengeData } from '@dexwallet/api-contract/platform';
import { authenticate, sendEmail } from '../../api/account';
import { Failure } from '../../auth/context';
import { randomRequestToken, session, type Session } from '../../auth/session';

const CODE_RESEND_MS = 60_000;

export default function EmailForm({ purpose, onSuccess }: { purpose?: 'bind' | 'replace'; onSuccess: () => void }) {
  const { t, i18n } = useTranslation();
  const [email, setEmail] = useState(''); const [code, setCode] = useState('');
  const [challenge, setChallenge] = useState<(ChallengeData & { email: string; sentAt: number }) | undefined>();
  const [now, setNow] = useState(Date.now()); const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>();
  const attempt = useRef<{ input: EmailLoginRequest | IdentityEmailRequest; ticket: Session } | undefined>(undefined);
  const active = useRef(true);
  useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);
  useEffect(() => { if (!challenge) return; const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [challenge]);
  const remaining = Math.max(0, Math.ceil(((challenge?.sentAt ?? 0) + CODE_RESEND_MS - now) / 1000));
  async function send() {
    setBusy(true); setError(undefined);
    try {
      const address = email.trim(); const locale = i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en';
      const result = await sendEmail(purpose ? { email: address, locale, purpose } : { email: address, locale });
      if (!active.current) return;
      setChallenge({ ...result, email: address, sentAt: Date.now() }); setCode(''); setNow(Date.now()); attempt.current = undefined;
    } catch (failure) { if (active.current) setError(failure); } finally { if (active.current) setBusy(false); }
  }
  async function verify() {
    if (!challenge) return;
    setBusy(true); setError(undefined);
    const old = attempt.current;
    if (!old || old.input.code !== code || old.input.challenge !== challenge.challenge) {
      const input: EmailLoginRequest = { requestToken: randomRequestToken(), email: challenge.email, challenge: challenge.challenge, code };
      const ticket = purpose ? session.capture() : await session.begin(session.capture());
      if (!ticket) { setBusy(false); return; }
      attempt.current = { input: purpose ? { ...input, purpose } : input, ticket };
    }
    const current = attempt.current;
    if (!current) return;
    try { await authenticate('email', current.input, current.ticket); if (active.current) onSuccess(); }
    catch (failure) { if (active.current) setError(failure); } finally { if (active.current) setBusy(false); }
  }
  return <form className="auth-form" onSubmit={event => { event.preventDefault(); void (challenge ? verify() : send()); }}>
    <label htmlFor="email-address">{t('email')}</label>
    <Input id="email-address" type="email" value={email} maxLength={512} autoComplete="email" disabled={busy || !!challenge} onChange={setEmail} />
    {challenge ? <>
      <p role="status">{t('codeSent', { email: challenge.email })}</p><p className="muted">{t('codeRules')}</p>
      <label htmlFor="email-code">{t('verificationCode')}</label>
      <Input id="email-code" value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" maxLength={6} disabled={busy} />
      {challenge.expiresAt * 1000 <= now && <p role="alert">{t('codeExpired')}</p>}
      <Button color="primary" type="submit" loading={busy} disabled={busy || !/^\d{6}$/.test(code) || challenge.expiresAt * 1000 <= now}>{t(purpose ? 'confirmBinding' : 'signIn')}</Button>
      <Button disabled={busy || remaining > 0} onClick={() => { void send(); }}>{remaining ? t('resendIn', { count: remaining }) : t('resendCode')}</Button>
      <Button fill="none" disabled={busy} onClick={() => { setChallenge(undefined); setCode(''); attempt.current = undefined; setError(undefined); }}>{t('changeEmail')}</Button>
    </> : <Button color="primary" type="submit" loading={busy} disabled={busy || !email.trim()}>{t('sendCode')}</Button>}
    {error !== undefined && <Failure error={error} />}
  </form>;
}
