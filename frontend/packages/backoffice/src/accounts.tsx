import { useEffect, useState } from 'react';
import { useTranslation } from '@dexwallet/i18n';
import { Button, Input, Space } from 'antd';
import { useAuth, Failure } from './auth';
import { Select, Table } from 'antd';
import type { BackofficeAccount, BackofficeCreateRequest, BackofficeResetRequest, BackofficeStatusRequest, BackofficeListRequestParams } from '@dexwallet/api-contract/platform';
import { decodeAccounts, decodeAccount, type Realm } from './client';
import { Paging } from './paging';
export function Accounts() {
  const { client } = useAuth();
  const { t } = useTranslation();
  const [realm, setRealm] = useState<Realm>('merchant'),
    [page, setPage] = useState(1),
    [revision, setRevision] = useState(0);
  const [rows, setRows] = useState<BackofficeAccount[]>([]),
    [loading, setLoading] = useState(true),
    [error, setError] = useState<unknown>();
  const [edit, setEdit] = useState<{
      action: 'create' | 'status' | 'reset';
      row?: BackofficeAccount;
    }>(),
    [email, setEmail] = useState(''),
    [password, setPassword] = useState(''),
    [reason, setReason] = useState(''),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    const c = new AbortController();
    let active = true;
    setLoading(true);
    setRows([]);
    setError(undefined);
    const params = { realm, page } satisfies BackofficeListRequestParams;
    void client
      .request(`/accounts?realm=${params.realm}&page=${params.page}`, decodeAccounts, {
        signal: c.signal,
      })
      .then(
        (v) => {
          if (active) setRows(v);
        },
        (e) => {
          if (active) setError(e);
        },
      )
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
      c.abort();
    };
  }, [client, realm, page, revision]);
  function open(next: typeof edit) {
    setEdit(next);
    setEmail('');
    setPassword('');
    setReason('');
    setError(undefined);
  }
  async function save() {
    if (!edit) return;
    setBusy(true);
    setError(undefined);
    try {
      if (edit.action === 'create')
        await client.request('/merchants', decodeAccount, {
          method: 'POST',
          body: { email, password, reason } satisfies BackofficeCreateRequest,
        });
      else if (edit.row)
        await client.request(`/accounts/${edit.action}`, decodeAccount, {
          method: 'POST',
          body: edit.action === 'reset'
            ? { realm, idx: edit.row.idx, reason, password } satisfies BackofficeResetRequest
            : { realm, idx: edit.row.idx, reason, status: edit.row.status === 'active' ? 'disabled' : 'active' } satisfies BackofficeStatusRequest,
        });
      open(undefined);
      setRevision((n) => n + 1);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="desktop-page">
      <h1>{t('boAccounts')}</h1>
      <Space wrap>
        <Select
          aria-label={t('boRealm')}
          value={realm}
          disabled={busy || !!edit}
          onChange={(v) => {
            setRealm(v);
            setPage(1);
          }}
          options={[
            { value: 'admin', label: t('boAdmin') },
            { value: 'merchant', label: t('boMerchant') },
          ]}
        />
        <Button
          disabled={busy || !!edit}
          onClick={() => open({ action: 'create' })}
        >
          {t('boCreateMerchant')}
        </Button>
        <Button onClick={() => setRevision((n) => n + 1)} disabled={busy}>
          {t('common:retry')}
        </Button>
      </Space>
      {realm === 'admin' && <p>{t('boSuperOnly')}</p>}
      <Table
        rowKey="idx"
        dataSource={rows}
        loading={loading}
        pagination={false}
        scroll={{ x: 750 }}
        columns={[
          { title: t('boEmail'), dataIndex: 'email' },
          { title: t('boIdx'), dataIndex: 'idx' },
          {
            title: t('boStatus'),
            render: (_, row) =>
              t(row.status === 'active' ? 'boActive' : 'boDisabled'),
          },
          {
            title: t('boActions'),
            render: (_, row) => (
              <Space>
                <Button
                  disabled={busy || !!edit}
                  onClick={() => open({ action: 'status', row })}
                >
                  {t(row.status === 'active' ? 'boDisable' : 'boEnable')}
                </Button>
                <Button
                  disabled={busy || !!edit}
                  onClick={() => open({ action: 'reset', row })}
                >
                  {t('boReset')}
                </Button>
              </Space>
            ),
          },
        ]}
      />
      <Paging
        page={page}
        count={rows.length}
        busy={loading || busy || !!edit}
        onPage={setPage}
      />
      {edit && (
        <form
          className="bo-form"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <h2>
            {t(
              edit.action === 'create'
                ? 'boCreateMerchant'
                : edit.action === 'reset'
                  ? 'boReset'
                  : 'boChangeStatus',
            )}
          </h2>
          {edit.row && <p>{edit.row.email}</p>}
          {edit.action === 'create' && (
            <>
              <label htmlFor="create-email">{t('boEmail')}</label>
              <Input
                id="create-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                disabled={busy}
                required
              />
            </>
          )}
          {edit.action !== 'status' && (
            <>
              <label htmlFor="create-password">
                {t('boTemporaryPassword')}
              </label>
              <Input.Password
                id="create-password"
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={busy}
                required
              />
              <p>{t('boPasswordRules')}</p>
            </>
          )}
          <label htmlFor="account-reason">{t('boReason')}</label>
          <Input.TextArea
            id="account-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            disabled={busy}
            maxLength={512}
            required
          />
          <Space>
            <Button
              type="primary"
              htmlType="submit"
              loading={busy}
              disabled={busy || !reason.trim()}
            >
              {t('boConfirm')}
            </Button>
            <Button disabled={busy} onClick={() => open(undefined)}>
              {t('boCancel')}
            </Button>
          </Space>
        </form>
      )}
      {error !== undefined && <Failure error={error} />}
    </section>
  );
}
