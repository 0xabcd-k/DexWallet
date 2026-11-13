import { useEffect, useState } from 'react';
import { useParams } from 'react-router';
import { Button } from 'antd-mobile';
import { formatAmount, useTranslation } from '@dexwallet/i18n';
import type { TransferData } from '@dexwallet/api-contract/platform';
import { getTransfer } from '../../api/transfers';
import { Failure, useAuth } from '../../auth/context';
export function TransferResult({ data }: { data: TransferData }) {
  const { t, i18n } = useTranslation();
  return <article className="transfer-result"><h2>{t('transferCompleted')}</h2><strong>{formatAmount(data.amount, 'USD')}</strong>
    <dl><dt>{t(data.direction === 'out' ? 'recipient' : 'sender')}</dt><dd>{data.counterparty.nickname || t('defaultNickname')}<br />{data.counterparty.idx}</dd>
      <dt>{t('transferFee')}</dt><dd>{formatAmount(data.fee, 'USD')}</dd><dt>{t('entryBusinessId')}</dt><dd>{data.idx}</dd><dt>{t('common:time')}</dt><dd>{new Date(data.createdAt).toLocaleString(i18n.resolvedLanguage)}</dd></dl></article>;
}
export default function TransferDetail() {
  const { idx = '' } = useParams(); const auth = useAuth(); const { t } = useTranslation();
  const [data, setData] = useState<TransferData>(); const [error, setError] = useState<unknown>(); const [revision, setRevision] = useState(0);
  useEffect(() => { const abort = new AbortController(); let active = true; setError(undefined); setData(undefined);
    void getTransfer(idx, abort.signal).then(d => { if (active) setData(d); }, e => { if (active) setError(e); });
    return () => { active = false; abort.abort(); };
  }, [idx, auth.session.generation, revision]);
  return <section className="transfer-page"><h1>{t('transferDetail')}</h1>{data && <TransferResult data={data} />}{error ? <><Failure error={error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></> : !data && <p role="status">{t('common:loading')}</p>}</section>;
}
