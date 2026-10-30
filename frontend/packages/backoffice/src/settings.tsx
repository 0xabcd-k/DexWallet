import { useState } from 'react';
import { useTranslation } from '@dexwallet/i18n';
import { Alert, Button, Input } from 'antd';
import { useAuth, Failure } from './auth';
import { useNavigate } from 'react-router';
export function Settings() {
  const auth = useAuth();
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [old, setOld] = useState(''),
    [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<unknown>(),
    [saved, setSaved] = useState(false);
  async function save() {
    setBusy(true);
    setError(undefined);
    setSaved(false);
    try {
      await auth.client.changePassword(old, password);
      setOld('');
      setPassword('');
      setSaved(true);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="desktop-page">
      <h1>{t('common:settings')}</h1>
      <h2>{t('boAccount')}</h2>
      <p>{auth.account?.email}</p>
      <p>{auth.account?.idx}</p>
      {auth.client.realm === 'merchant' && <p>{t('boPendingMerchant')}</p>}
      {auth.account?.mustChangePassword && (
        <Alert type="warning" title={t('boMustChange')} />
      )}
      <h2>{t('boChangePassword')}</h2>
      <p>{t('boPasswordRules')}</p>
      <form
        className="bo-form"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <label htmlFor="bo-old">{t('boOldPassword')}</label>
        <Input.Password
          id="bo-old"
          autoComplete="current-password"
          value={old}
          onChange={(e) => setOld(e.target.value)}
          disabled={busy}
          required
        />
        <label htmlFor="bo-new">{t('boNewPassword')}</label>
        <Input.Password
          id="bo-new"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={busy}
          required
        />
        <Button
          type="primary"
          htmlType="submit"
          loading={busy}
          disabled={busy || !old || !password}
        >
          {t('boSave')}
        </Button>
      </form>
      {saved && <p role="status">{t('boSaved')}</p>}
      {error !== undefined && <Failure error={error} />}
      <Button
        className="bo-signout"
        onClick={() => {
          void auth.client.session
            .clear()
            .then(() => navigate('/login', { replace: true }));
        }}
      >
        {t('boSignOut')}
      </Button>
      {!auth.session.persistent && <p role="status">{t('boMemory')}</p>}
    </section>
  );
}
