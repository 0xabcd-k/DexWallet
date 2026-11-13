import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { Button, Input, Popup, Tabs } from 'antd-mobile';
import { ApiError } from '@dexwallet/api-client';
import { formatAmount, useTranslation } from '@dexwallet/i18n';
import type { CurrentUserData, TransferData, TransferPolicyData } from '@dexwallet/api-contract/platform';
import { AccountKind, getBalances } from '../../api/balances';
import { cents, findTransfer, getTransferPolicy, resolveTarget, submitTransfer } from '../../api/transfers';
import { Failure, useAuth } from '../../auth/context';
import { session, type Session } from '../../auth/session';
import { clearPending, preparePending, readPending, type PendingTransfer } from './pending';
import { Scanner } from './scanner';
import { TransferResult } from './detail';
import './style.less';
interface Review { target: CurrentUserData; policy: TransferPolicyData; balance: string; amount: string; ticket: Session }
// These reject a new operation before commit. Unknown recovery never discards
// its saved key even when a subsequent request is rejected.
const definiteRejections = new Set(['PAYMENT_RESTRICTED', 'TRANSFER_AMOUNT_LIMIT', 'TRANSFER_DAILY_LIMIT', 'INSUFFICIENT_BALANCE', 'TRANSFER_RECIPIENT_UNAVAILABLE', 'INVALID_ARGUMENT']);
export default function Pay() { const auth = useAuth(); return <Payment key={auth.session.generation} />; }
function Payment() {
  const auth = useAuth(); const { t, i18n } = useTranslation(); const [params, setParams] = useSearchParams();
  const code = params.get('code') ?? '';
  const invalidLink = [...params.keys()].some(k => k !== 'code' && k !== 'manual') || params.getAll('code').length > 1;
  const owner = `${auth.session.credential!.issuer}:${auth.session.credential!.idx}`;
  const [scanning, setScanning] = useState(!code && !params.has('manual'));
  const [recipient, setRecipient] = useState(''); const [amount, setAmount] = useState('');
  const [raw, setRaw] = useState(code); const [manual, setManual] = useState(!code);
  const [review, setReview] = useState<Review>(); const [pending, setPending] = useState<PendingTransfer>();
  const [result, setResult] = useState<TransferData>(); const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false); const [storageError, setStorageError] = useState(false);
  const [unknown, setUnknown] = useState(false); const [now, setNow] = useState(Date.now());
  const active = useRef(true); const running = useRef(false); const targetVersion = useRef(0);
  useEffect(() => {
    active.current = true;
    try { const saved = readPending(localStorage, owner); if (saved) { setPending(saved); setScanning(false); void recover(saved); } }
    catch { setStorageError(true); setScanning(false); }
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => { active.current = false; clearInterval(timer); };
  }, []);
  useEffect(() => { targetVersion.current++; setReview(undefined); if (code) { setRaw(code); setManual(false); setScanning(false); setReview(undefined); } }, [code]);
  async function complete(data: TransferData, saved: PendingTransfer) {
    if (data.direction !== 'out' || data.counterparty.idx !== saved.recipientIdx || cents(data.amount) !== cents(saved.amount)) throw new ApiError('protocol');
    clearPending(localStorage, owner, saved.requestKey);
    if (active.current) { setPending(undefined); setResult(data); setReview(undefined); setUnknown(false); }
  }
  async function recover(saved: PendingTransfer) {
    if (running.current) return; running.current = true; setBusy(true); setError(undefined);
    try { const data = await findTransfer(saved.requestKey, session.capture()); await complete(data, saved); }
    catch (e) { if (active.current) { setUnknown(true); if (!(e instanceof ApiError && e.code === 'NOT_FOUND')) setError(e); } }
    finally { running.current = false; if (active.current) setBusy(false); }
  }
  async function preview(saved?: PendingTransfer) {
    if (running.current) return; running.current = true; setBusy(true); setError(undefined); setReview(undefined);
    const ticket = session.capture(); const version = targetVersion.current;
    try {
      const targetInput = saved ? { recipientIdx: saved.recipientIdx } : manual ? { recipientIdx: recipient.trim() } : { receiveCode: raw.trim() };
      const [target, policy, accounts] = await Promise.all([resolveTarget(targetInput, ticket), getTransferPolicy(ticket), getBalances(new AbortController().signal)]);
      const balance = accounts.find(a => a.kind === AccountKind.usd)!.balance;
      if (active.current && session.matches(ticket) && version === targetVersion.current) setReview({ target, policy, balance, amount: saved?.amount ?? amount, ticket });
    } catch (e) { if (active.current) setError(e); }
    finally { running.current = false; if (active.current) setBusy(false); }
  }
  const value = review && cents(review.amount);
  const expired = review && now >= Date.parse(review.policy.resetAt);
  const restriction = review?.policy.restrictedUntil && now < Date.parse(review.policy.restrictedUntil);
  const problem = !review ? '' : value === undefined || value <= 0n ? 'invalidTransferAmount'
    : restriction ? 'transferRestricted' : expired ? 'reviewExpired'
    : value < (cents(review.policy.plan.minAmount) ?? 0n) || value > (cents(review.policy.plan.maxAmount) ?? 0n) ? 'transferAmountLimit'
    : value > (cents(review.policy.remainingToday) ?? 0n) ? 'transferDailyLimit'
    : value > (cents(review.balance) ?? 0n) ? 'transferInsufficient' : '';
  async function confirm() {
    if (!review || problem || running.current || !session.matches(review.ticket)) return;
    running.current = true; setBusy(true); setError(undefined);
    let saved = pending; const recovering = !!pending;
    try {
      if (!saved) {
        let prepared;
        try { prepared = await preparePending(owner, review.target.idx, review.amount); }
        catch { setStorageError(true); return; }
        saved = prepared.request; setPending(saved);
        if (prepared.existing) { setReview(undefined); setUnknown(true); return; }
      }
      if (!session.matches(review.ticket)) return;
      const data = await submitTransfer(saved, review.ticket);
      await complete(data, saved);
    } catch (e) {
      if (saved && !recovering && e instanceof ApiError && definiteRejections.has(e.code ?? '')) {
        try { clearPending(localStorage, owner, saved.requestKey); setPending(undefined); } catch { setStorageError(true); }
      } else { setUnknown(true); }
      if (active.current) { setReview(undefined); setError(e); }
    } finally { running.current = false; if (active.current) setBusy(false); }
  }
  return <section className="transfer-page"><h1>{t('pay')}</h1>
    {result ? <><TransferResult data={result} /><Link to={`/transfers/${result.idx}`}>{t('transferDetail')}</Link><Button onClick={() => { setResult(undefined); setAmount(''); setRecipient(''); setRaw(''); setParams({ manual: '1' }); setManual(true); }}>{t('anotherTransfer')}</Button></> : pending ? <div className="transfer-pending"><h2>{t('pendingTransfer')}</h2>
      <p>{t('recipient')}: {pending.recipientIdx}</p><p>{formatAmount(pending.amount, 'USD')}</p>
      {unknown && <p role="status">{t('unknownTransfer')}</p>}
      <Button disabled={busy} onClick={() => { void recover(pending); }}>{t('recoverTransfer')}</Button>
      <Button disabled={busy || storageError} onClick={() => { void preview(pending); }}>{t('reviewOriginal')}</Button>
    </div> : invalidLink ? <p role="alert">{t('invalidReceiveLink')}</p> : <>
      {scanning ? <><Scanner onScan={value => { setRaw(value); setManual(false); setScanning(false); setReview(undefined); }} />
        <Button onClick={() => { setScanning(false); setManual(true); }}>{t('manualTransfer')}</Button></> : <>
        <Button disabled={busy} onClick={() => { setScanning(true); setReview(undefined); }}>{t('scanReceive')}</Button>
        <Tabs onChange={() => setReview(undefined)}><Tabs.Tab title={t('transferBalance')} key="balance" disabled={busy}>
          <form onSubmit={e => { e.preventDefault(); void preview(); }}>
            <label htmlFor="transfer-target">{t(manual ? 'recipientIdx' : 'receiveLink')}</label>
            <Input id="transfer-target" value={manual ? recipient : raw} onChange={v => { if (manual) setRecipient(v); else setRaw(v); setReview(undefined); }} maxLength={manual ? 20 : 2048} disabled={busy} />
            <Button size="small" disabled={busy} onClick={() => { setManual(v => !v); setReview(undefined); }}>{t(manual ? 'useReceiveLink' : 'useRecipientIdx')}</Button>
            <label htmlFor="transfer-amount">{t('transferAmount')}</label><Input id="transfer-amount" inputMode="decimal" value={amount} onChange={v => { setAmount(v); setReview(undefined); }} disabled={busy} maxLength={21} />
            <Button type="submit" color="primary" disabled={busy || storageError || (cents(amount) ?? 0n) <= 0n || !(manual ? recipient : raw).trim()}>{t('reviewPayment')}</Button>
          </form>
        </Tabs.Tab><Tabs.Tab title={t('transferChain')} key="chain" disabled={busy}><p>{t(auth.launch.mode === 'telegram' ? 'chainRequiresApp' : 'chainPending')}</p></Tabs.Tab></Tabs>
      </>}
    </>}
    {storageError && <p role="alert">{t('paymentStorageUnavailable')}</p>}
    {error !== undefined && <Failure error={error} />}{busy && <p role="status">{t('common:loading')}</p>}
    <Popup visible={!!review} onMaskClick={() => { if (!busy) setReview(undefined); }} bodyStyle={{ borderTopLeftRadius: 16, borderTopRightRadius: 16 }}>
      {review && <div className="payment-review"><h2>{t('reviewPayment')}</h2><p>{review.target.nickname || t('defaultNickname')}</p><p className="transfer-idx">{review.target.idx}</p>
        <strong>{formatAmount(review.amount, 'USD')}</strong><p>{t('payAsset')}: {t('usdBalance')}</p><p>{t('transferFee')}: 0.00 USD</p>
        <p>{t('availableBalance')}: <span className={problem === 'transferInsufficient' ? 'auth-error' : ''}>{formatAmount(review.balance, 'USD')}</span></p>
        <p>{t('limitSummary', { min: review.policy.plan.minAmount, max: review.policy.plan.maxAmount, remaining: review.policy.remainingToday })}</p>
        <p>{t('limitReset', { time: new Date(review.policy.resetAt).toLocaleString(i18n.resolvedLanguage) })}</p>
        {problem && <p role="alert">{t(problem)}</p>}{restriction && <p>{new Date(review.policy.restrictedUntil).toLocaleString(i18n.resolvedLanguage)}</p>}
        <Button block color="primary" loading={busy} disabled={busy || !!problem || storageError} onClick={() => { void confirm(); }}>{t('confirmPayment')}</Button>
        <Button block disabled={busy} onClick={() => setReview(undefined)}>{t('cancel')}</Button>
      </div>}
    </Popup>
  </section>;
}
