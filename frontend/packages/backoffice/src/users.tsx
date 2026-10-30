import { useState } from 'react';
import { useTranslation } from '@dexwallet/i18n';
import { Button, Input } from 'antd';
import { useAuth, Failure } from './auth';
import type { ManagedUserData, ManagedUserStatusRequest, ManagedUserRequestParams } from '@dexwallet/api-contract/platform';
import { decodeUser } from './client';
export function Users() {
  const { client } = useAuth();
  const { t } = useTranslation();
  const [idx, setIdx] = useState(''),
    [user, setUser] = useState<ManagedUserData>(),
    [reason, setReason] = useState(''),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<unknown>();
  async function find() {
    setBusy(true);
    setError(undefined);
    setUser(undefined);
    try {
      const params = { idx } satisfies ManagedUserRequestParams;
      setUser(
        await client.request(
          `/user?idx=${encodeURIComponent(params.idx)}`,
          decodeUser,
        ),
      );
      setReason('');
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  async function update() {
    if (!user) return;
    setBusy(true);
    setError(undefined);
    try {
      setUser(
        await client.request('/user/status', decodeUser, {
          method: 'POST',
          body: {
            idx: user.idx,
            status: user.status === 'active' ? 'disabled' : 'active',
            reason,
          } satisfies ManagedUserStatusRequest,
        }),
      );
      setReason('');
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="desktop-page">
      <h1>{t('boUsers')}</h1>
      <form
        className="bo-form"
        onSubmit={(e) => {
          e.preventDefault();
          void find();
        }}
      >
        <label htmlFor="user-idx">{t('boIdx')}</label>
        <Input
          id="user-idx"
          value={idx}
          onChange={(e) => setIdx(e.target.value)}
          disabled={busy}
          required
        />
        <Button htmlType="submit" loading={busy} disabled={busy}>
          {t('boSearch')}
        </Button>
      </form>
      {user && (
        <form
          className="bo-form"
          onSubmit={(e) => {
            e.preventDefault();
            void update();
          }}
        >
          <p>
            {user.nickname} · {user.idx} ·{' '}
            {t(user.status === 'active' ? 'boActive' : 'boDisabled')}
          </p>
          <label htmlFor="user-reason">{t('boReason')}</label>
          <Input.TextArea
            id="user-reason"
            maxLength={512}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            disabled={busy}
            required
          />
          <Button htmlType="submit" disabled={busy || !reason.trim()}>
            {t(user.status === 'active' ? 'boDisable' : 'boEnable')}
          </Button>
        </form>
      )}
      {error !== undefined && <Failure error={error} />}
    </section>
  );
}
