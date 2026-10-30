import { useEffect, useState } from 'react';
import { useTranslation } from '@dexwallet/i18n';
import { Button } from 'antd';
import { useAuth, Failure } from './auth';
import { Table } from 'antd';
import type { BackofficeAuditItem, BackofficeAuditRequestParams } from '@dexwallet/api-contract/platform';
import { decodeAudits } from './client';
import { Paging } from './paging';
export function Audits() {
  const { client } = useAuth();
  const { t, i18n } = useTranslation();
  const [page, setPage] = useState(1),
    [revision, setRevision] = useState(0),
    [rows, setRows] = useState<BackofficeAuditItem[]>([]),
    [busy, setBusy] = useState(true),
    [error, setError] = useState<unknown>();
  useEffect(() => {
    const c = new AbortController();
    let active = true;
    setBusy(true);
    setError(undefined);
    setRows([]);
    const params = { page } satisfies BackofficeAuditRequestParams;
    void client
      .request(`/audits?page=${params.page}`, decodeAudits, { signal: c.signal })
      .then(
        (v) => {
          if (active) setRows(v);
        },
        (e) => {
          if (active) setError(e);
        },
      )
      .finally(() => {
        if (active) setBusy(false);
      });
    return () => {
      active = false;
      c.abort();
    };
  }, [client, page, revision]);
  const displayValue = (row: BackofficeAuditItem, value: string) => {
    if (row.action === 'change_status' || row.action === 'create_account') {
      if (value === 'active') return t('boActive');
      if (value === 'disabled') return t('boDisabled');
    }
    return value || '—';
  };
  return (
    <section className="desktop-page">
      <h1>{t('boAudits')}</h1>
      <Button onClick={() => setRevision((n) => n + 1)} disabled={busy}>
        {t('common:retry')}
      </Button>
      <Table
        rowKey="id"
        dataSource={rows}
        loading={busy}
        pagination={false}
        scroll={{ x: 1000 }}
        columns={[
          {
            title: t('common:time'),
            render: (_, r) =>
              new Date(r.createdAt * 1000).toLocaleString(
                i18n.resolvedLanguage,
              ),
          },
          {
            title: t('boActor'),
            render: (_, r) => `${t('boRealm_' + r.actorRealm)} ${r.actorIdx}`,
          },
          { title: t('boAction'), render: (_, r) => t('boAction_' + r.action) },
          {
            title: t('boTarget'),
            render: (_, r) =>
              `${t('boRealm_' + r.targetRealm)} ${r.targetEmail || r.targetIdx}`,
          },
          { title: t('boResult'), render: (_, r) => t('boResult_' + r.result) },
        ]}
        expandable={{
          expandedRowRender: (r) => (
            <div>
              <p>
                {t('boBefore')}: {displayValue(r, r.beforeValue)}
              </p>
              <p>
                {t('boAfter')}: {displayValue(r, r.afterValue)}
              </p>
              <p>
                {t('boReason')}: {r.reason || '—'}
              </p>
              <p>
                {t('boTrace')}: {r.traceId}
              </p>
              <p>
                {t('boIdx')}: {r.targetIdx || '—'}
              </p>
            </div>
          ),
        }}
      />
      <Paging page={page} count={rows.length} busy={busy} onPage={setPage} />
      {error !== undefined && <Failure error={error} />}
    </section>
  );
}
