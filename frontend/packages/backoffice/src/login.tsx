import { useEffect, useState } from 'react';
import { useTranslation } from '@dexwallet/i18n';
import { Button, Input, Space } from 'antd';
import { useAuth, Failure } from './auth';
import { Navigate, useLocation } from 'react-router';
import { LanguageControl } from '@dexwallet/i18n';
import type { ChallengeData } from '@dexwallet/api-contract/platform';
export function safeReturn(raw: string | null): string {
  if (!raw) return '/overview';
  try {
    const u = new URL(raw, 'https://backoffice.invalid');
    if (
      u.origin !== 'https://backoffice.invalid' ||
      !['/overview', '/settings', '/accounts', '/users', '/audits'].includes(
        u.pathname,
      )
    )
      return '/overview';
    return u.pathname + u.search;
  } catch {
    return '/overview';
  }
}
export function Login() {
  const { client, session } = useAuth();
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const target = safeReturn(
    new URLSearchParams(location.search).get('returnTo'),
  );
  const [email, setEmail] = useState(''),
    [password, setPassword] = useState(''),
    [code, setCode] = useState('');
  const [challenge, setChallenge] = useState<ChallengeData>();
  const [sentAt, setSentAt] = useState(0),
    [now, setNow] = useState(Date.now());
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<unknown>();
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  if (session.credential) return <Navigate to={target} replace />;
  const remaining = Math.max(0, Math.ceil((sentAt + 60000 - now) / 1000));
  async function send() {
    setBusy(true);
    setError(undefined);
    try {
      const next = await client.sendCode(
        email,
        password,
        i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en',
      );
      setChallenge(next);
      setCode('');
      setSentAt(Date.now());
      setNow(Date.now());
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  async function submit() {
    if (!challenge) return;
    const ticket = client.session.capture();
    setBusy(true);
    setError(undefined);
    try {
      await client.login(email, password, challenge.challenge, code, ticket);
      setPassword('');
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="bo-login">
      <LanguageControl app={client.realm} />
      <h1>
        {t(client.realm === 'admin' ? 'boAdminLogin' : 'boMerchantLogin')}
      </h1>
      <p>{t('boLoginBody')}</p>
      <form
        className="bo-form"
        onSubmit={(e) => {
          e.preventDefault();
          void (challenge ? submit() : send());
        }}
      >
        <label htmlFor="bo-email">{t('boEmail')}</label>
        <Input
          id="bo-email"
          type="email"
          autoComplete="username"
          value={email}
          disabled={busy || !!challenge}
          onChange={(e) => setEmail(e.target.value)}
          required
          maxLength={254}
        />
        <label htmlFor="bo-password">{t('boPassword')}</label>
        <Input.Password
          id="bo-password"
          autoComplete="current-password"
          value={password}
          disabled={busy || !!challenge}
          onChange={(e) => setPassword(e.target.value)}
          required
          maxLength={72}
        />
        {challenge && (
          <>
            <label htmlFor="bo-code">{t('boCode')}</label>
            <Input
              id="bo-code"
              value={code}
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={6}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
              disabled={busy}
            />
            <p role="status">
              {t(
                now >= challenge.expiresAt * 1000 ? 'boExpired' : 'boCodeBody',
              )}
            </p>
          </>
        )}
        <Button
          type="primary"
          htmlType="submit"
          aria-label={t(challenge ? 'boSignIn' : 'boSendCode')}
          loading={busy}
          disabled={
            busy ||
            !email ||
            !password ||
            (!!challenge &&
              (code.length !== 6 || now >= challenge.expiresAt * 1000))
          }
        >
          {t(challenge ? 'boSignIn' : 'boSendCode')}
        </Button>
        {challenge && (
          <Space wrap>
            <Button
              disabled={busy || remaining > 0}
              onClick={() => {
                void send();
              }}
            >
              {remaining > 0
                ? t('boResendIn', { count: remaining })
                : t('boResend')}
            </Button>
            <Button
              disabled={busy}
              onClick={() => {
                setChallenge(undefined);
                setCode('');
                setError(undefined);
              }}
            >
              {t('boEditLogin')}
            </Button>
          </Space>
        )}
      </form>
      {error !== undefined && <Failure error={error} />}
      <p>{t('boNoRegistration')}</p>
      {!session.persistent && <p role="status">{t('boMemory')}</p>}
    </main>
  );
}
