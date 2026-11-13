import { useEffect, useState } from 'react';
import { Button, Input } from 'antd';
import { ApiError, decodeData } from '@dexwallet/api-client';
import { randomToken } from '@dexwallet/session';
import { useTranslation } from '@dexwallet/i18n';
import type { CreateLimitPlanRequest, LimitPlansData, TransferLimitPlan, TransferPolicyData } from '@dexwallet/api-contract/platform';
import { Failure, useAuth } from './auth';
function PlanSummary({ plan }: { plan: TransferLimitPlan }) {
  const { t } = useTranslation(); return <p>{plan.name} · {plan.idx}<br />{t('boPlanAmounts', { min: plan.minAmount, max: plan.maxAmount, daily: plan.dailyOutgoingLimit })}</p>;
}
export function Limits() {
  const { client } = useAuth(); const { t } = useTranslation();
  const [page, setPage] = useState(1); const [revision, setRevision] = useState(0);
  const [plans, setPlans] = useState<TransferLimitPlan[]>([]); const [error, setError] = useState<unknown>(); const [busy, setBusy] = useState(false);
  const [form, setForm] = useState<CreateLimitPlanRequest>({ name: '', minAmount: '0.01', maxAmount: '100.00', dailyOutgoingLimit: '500.00', requestKey: randomToken() });
  const [submitted, setSubmitted] = useState(false);
  useEffect(() => { let active = true; const abort = new AbortController(); setBusy(true); setError(undefined);
    void client.request(`/transfer-limit-plans?page=${page}`, decodeData<LimitPlansData>, { signal: abort.signal }).then(d => { if (active) setPlans(d.plans); }, e => { if (active) setError(e); }).finally(() => { if (active) setBusy(false); });
    return () => { active = false; abort.abort(); };
  }, [client, page, revision]);
  async function create() {
    setBusy(true); setError(undefined); setSubmitted(true);
    try { await client.request('/transfer-limit-plans', decodeData<TransferLimitPlan>, { method: 'POST', body: form }); setSubmitted(false); setForm({ ...form, name: '', requestKey: randomToken() }); setPage(1); setRevision(n => n + 1); }
    catch (e) { setError(e); if (e instanceof ApiError && e.code === 'INVALID_ARGUMENT') setSubmitted(false); } finally { setBusy(false); }
  }
  return <section className="desktop-page"><h1>{t('boLimits')}</h1><p>{t('boImmutablePlans')}</p>
    <form className="bo-form" onSubmit={e => { e.preventDefault(); void create(); }}>
      {(['name', 'minAmount', 'maxAmount', 'dailyOutgoingLimit'] as const).map(key => <label key={key}>{t(`boPlan_${key}`)}<Input value={form[key]} onChange={e => setForm({ ...form, [key]: e.target.value })} disabled={busy || submitted} required maxLength={key === 'name' ? 128 : 21} /></label>)}
      <Button htmlType="submit" disabled={busy || !form.name.trim()}>{t(submitted ? 'boRetryPlan' : 'boCreatePlan')}</Button>
      {submitted && <p>{t('boPlanUnknown')}</p>}
    </form>
    <ul>{plans.map(plan => <li key={plan.idx}><PlanSummary plan={plan} /></li>)}</ul>
    <Button disabled={busy || page === 1} onClick={() => setPage(p => p - 1)}>{t('boPrevious')}</Button><Button disabled={busy || plans.length < 50} onClick={() => setPage(p => p + 1)}>{t('boNext')}</Button>
    {error !== undefined && <><Failure error={error} /><Button onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
  </section>;
}
export function UserLimitPlan({ idx }: { idx: string }) {
  const { client } = useAuth(); const { t, i18n } = useTranslation();
  const [policy, setPolicy] = useState<TransferPolicyData>(); const [plans, setPlans] = useState<TransferLimitPlan[]>([]);
  const [page, setPage] = useState(1); const [revision, setRevision] = useState(0); const [selected, setSelected] = useState(''); const [reason, setReason] = useState('');
  const [error, setError] = useState<unknown>(); const [busy, setBusy] = useState(false);
  useEffect(() => { let active = true; const abort = new AbortController(); setBusy(true); setError(undefined);
    void Promise.all([client.request(`/users/${idx}/transfer-limit-plan`, decodeData<TransferPolicyData>, { signal: abort.signal }), client.request(`/transfer-limit-plans?page=${page}`, decodeData<LimitPlansData>, { signal: abort.signal })]).then(([p, list]) => { if (active) { setPolicy(p); setPlans(list.plans); setSelected(''); } }, e => { if (active) setError(e); }).finally(() => { if (active) setBusy(false); });
    return () => { active = false; abort.abort(); };
  }, [idx, client, page, revision]);
  const next = plans.find(p => p.idx === selected);
  async function assign() {
    if (!policy || !next) return; setBusy(true); setError(undefined);
    try { const p = await client.request(`/users/${idx}/transfer-limit-plan`, decodeData<TransferPolicyData>, { method: 'PUT', body: { planIdx: next.idx, expectedPlanIdx: policy.plan.idx, reason } }); setPolicy(p); setReason(''); setSelected(''); }
    catch (e) { setError(e); } finally { setBusy(false); }
  }
  return <section className="bo-form"><h2>{t('boUserLimit')}</h2>{policy && <><h3>{t('boBefore')}</h3><PlanSummary plan={policy.plan} /><p>{t('boPlanUsage', { used: policy.usedToday, remaining: policy.remainingToday, reset: new Date(policy.resetAt).toLocaleString(i18n.resolvedLanguage) })}</p></>}
    <label>{t('boChoosePlan')}<select value={selected} disabled={busy} onChange={e => setSelected(e.target.value)}><option value="">{t('boChoosePlan')}</option>{plans.map(p => <option key={p.idx} value={p.idx}>{p.name} · {p.idx}</option>)}</select></label>
    <div><Button disabled={busy || page === 1} onClick={() => setPage(p => p - 1)}>{t('boPrevious')}</Button><Button disabled={busy || plans.length < 50} onClick={() => setPage(p => p + 1)}>{t('boNext')}</Button></div>
    {next && <><h3>{t('boAfter')}</h3><PlanSummary plan={next} /></>}
    <label>{t('boReason')}<Input.TextArea value={reason} onChange={e => setReason(e.target.value)} maxLength={512} disabled={busy} /></label>
    <Button disabled={busy || !next || !reason.trim() || next.idx === policy?.plan.idx} onClick={() => { void assign(); }}>{t('boAssignPlan')}</Button>
    <Button disabled={busy} onClick={() => setRevision(n => n + 1)}>{t('boRefreshPlan')}</Button>
    {error !== undefined && <Failure error={error} />}
  </section>;
}
