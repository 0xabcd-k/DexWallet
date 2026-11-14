import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { Button } from 'antd-mobile';
import { ApiError } from '@dexwallet/api-client';
import { useTranslation } from '@dexwallet/i18n';
import type { ClaimPreviewData, TaskCompletionData } from '@dexwallet/api-contract/platform';
import { claimByRequest, createInvitation, getClaimPreview, submitClaim, type ClaimSubmission } from '../../api/task-rewards';
import { Failure, useAuth } from '../../auth/context';
import { clearClaim, prepareClaim, readClaim } from './pending';
import { CompletionResult } from './results';

// These responses require a fresh context. Busy/network/verification failures
// retain the original request; they must never silently create a second claim.
const reviewCodes = new Set(['TASK_CONTEXT_CHANGED', 'TASK_BINDING_CHANGED', 'TASK_INVITATION_INVALID', 'TASK_ALREADY_COMPLETED']);
export function ClaimPanel({ idx, invitationIdx, refresh }: { idx: string; invitationIdx: string; refresh: () => void }) {
  const auth = useAuth(); const userIdx = auth.session.credential!.idx; const { t } = useTranslation();
  const [preview, setPreview] = useState<ClaimPreviewData>(); const [revision, setRevision] = useState(0);
  const [previewError, setPreviewError] = useState<unknown>(); const [error, setError] = useState<unknown>();
  const [pending, setPending] = useState<ClaimSubmission>(); const [result, setResult] = useState<TaskCompletionData>();
  const [confirmed, setConfirmed] = useState(false); const [busy, setBusy] = useState(false); const [link, setLink] = useState('');
  const operation = useRef<AbortController | undefined>(undefined);
  const refreshRef = useRef(refresh); refreshRef.current = refresh;
  useEffect(() => {
    const controller = new AbortController(); setPreview(undefined); setPreviewError(undefined); setConfirmed(false); setLink('');
    void getClaimPreview(idx, invitationIdx, controller.signal).then(p => { if (!controller.signal.aborted) setPreview(p); }, e => { if (!controller.signal.aborted) setPreviewError(e); });
    return () => controller.abort();
  }, [idx, invitationIdx, revision]);
  useEffect(() => {
    try { setPending(readClaim(localStorage, userIdx)); } catch (e) { setError(e); }
    return () => operation.current?.abort();
  }, [userIdx]);
  async function accept(data: TaskCompletionData, request: ClaimSubmission, controller: AbortController) {
    if (data.taskIdx !== request.idx || data.mode !== request.mode || data.participantStage !== request.expectedParticipantStage) throw new Error('Mismatched claim result');
    if (controller.signal.aborted) return;
    await clearClaim(userIdx, request.requestKey);
    if (!controller.signal.aborted) { setPending(undefined); setResult(data); setRevision(n => n + 1); refreshRef.current(); }
  }
  async function execute(request: ClaimSubmission, lookup: boolean, controller: AbortController) {
    try { await accept(await (lookup ? claimByRequest(request.requestKey, controller.signal) : submitClaim(request, controller.signal)), request, controller); }
    catch (e) {
      if (controller.signal.aborted) return;
      setError(e);
      if (!lookup && e instanceof ApiError && reviewCodes.has(e.code ?? '')) {
        // Look for a committed result before releasing the old confirmation.
        try { await accept(await claimByRequest(request.requestKey, controller.signal), request, controller); return; }
        catch (lookupError) {
          if (controller.signal.aborted) return;
          if (lookupError instanceof ApiError && lookupError.code === 'NOT_FOUND') {
            await clearClaim(userIdx, request.requestKey);
            if (!controller.signal.aborted) { setPending(undefined); setRevision(n => n + 1); refreshRef.current(); }
          }
        }
      }
    }
  }
  async function run(work: (controller: AbortController) => Promise<void>) {
    if (operation.current && !operation.current.signal.aborted) return;
    const controller = new AbortController(); operation.current = controller; setBusy(true); setError(undefined);
    try { await work(controller); } catch (e) { if (!controller.signal.aborted) setError(e); }
    finally { if (!controller.signal.aborted) setBusy(false); controller.abort(); }
  }
  async function claim(controller: AbortController) {
    if (!preview || !preview.task.claimAvailable || (preview.mode === 'help' && !confirmed)) return;
    const saved = await prepareClaim(userIdx, { idx, mode: preview.mode, invitationIdx: preview.invitationIdx, expectedTaskRevision: preview.taskRevision, expectedIdentityRevision: preview.identityRevision, expectedParticipantStage: preview.participantStage });
    if (controller.signal.aborted) return;
    setPending(saved.request);
    if (!saved.existing) await execute(saved.request, false, controller);
  }
  return <section className="claim-panel" aria-label={t('claimPanel')}>
    {result && <CompletionResult data={result} />}
    {pending ? <div role="status"><p>{t('claimPending')}</p>
      <Link to={`/tasks/${pending.idx}${pending.invitationIdx ? `?invitationIdx=${pending.invitationIdx}` : ''}`}>{t('claimOriginal')}</Link>
      <Button disabled={busy} onClick={() => { void run(c => execute(pending, true, c)); }}>{t('claimLookup')}</Button>
      <Button disabled={busy} onClick={() => { void run(c => execute(pending, false, c)); }}>{t('claimRetry')}</Button>
    </div> : !result && preview && preview.task.eligibility !== 'completed' && <>
      <h2>{t('claimEstimate')}</h2>
      {preview.mode === 'help' && <p>{t('claimInviter', { name: preview.inviterNickname || preview.inviterIdx })}</p>}
      <dl><dt>{t('rewardCash')}</dt><dd>${preview.rewardUsd}</dd><dt>{t('taskPoints')}</dt><dd>{preview.taskPoints}</dd>
        <dt>{t('claimCommission')}</dt><dd>{preview.commissionPoints}</dd><dt>{t('claimProDelta')}</dt><dd>{preview.proDelta}</dd></dl>
      {preview.participantStage === 'first' && <p>{t('claimFirstNotice')}</p>}
      {preview.mode === 'help' && <label className="claim-confirm"><input type="checkbox" checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />{t(preview.participantStage === 'first' ? 'claimFirstHelpConfirm' : 'claimOldHelpConfirm')}</label>}
      <Button color="primary" disabled={busy || !preview.task.claimAvailable || (preview.mode === 'help' && !confirmed)} loading={busy} onClick={() => { void run(claim); }}>{t(preview.mode === 'help' ? 'claimHelp' : 'claimSubmit')}</Button>
      <p>{t('claimEstimateNotice')}</p>
    </>}
    {previewError !== undefined && <><Failure error={previewError} /><Button disabled={busy} onClick={() => setRevision(n => n + 1)}>{t('common:retry')}</Button></>}
    {error !== undefined && <Failure error={error} />}
    {!preview && previewError === undefined && <p role="status">{t('common:loading')}</p>}
    {invitationIdx && !pending && <Link to={`/tasks/${idx}`}>{t('claimSelfInstead')}</Link>}
    {preview?.canInvite && <Button disabled={busy} onClick={() => { void run(async controller => {
      const invitation = await createInvitation(idx, controller.signal);
      if (!controller.signal.aborted) { const target = new URL(location.href); target.search = ''; target.hash = invitation.path; setLink(auth.launch.mode === 'telegram' && invitation.telegramUrl ? invitation.telegramUrl : invitation.url || target.toString()); }
    }); }}>{t('claimCreateInvite')}</Button>}
    {link && <label>{t('claimInviteLink')}<input readOnly value={link} onFocus={e => e.currentTarget.select()} /></label>}
    <p><Link to={`/task-completions?taskIdx=${idx}`}>{t('claimHistory')}</Link></p>
  </section>;
}
