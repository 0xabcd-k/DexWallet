import { useEffect, useState } from 'react';
import { Button, Input } from 'antd';
import { ApiError, decodeData } from '@dexwallet/api-client';
import type { ManagedTaskData, ManagedTasksData, ManagedCompletionsData, SaveTaskDraftRequest } from '@dexwallet/api-contract/platform';
import { randomToken } from '@dexwallet/session';
import { useTranslation } from '@dexwallet/i18n';
import { Failure, useAuth } from './auth';
const DraftStatus = 'draft';
const ActiveStatus = 'active';
export function ManagedTasks() { const { session } = useAuth(); return <Tasks key={session.generation}/>; }
function Tasks() {
    const { client, session } = useAuth();
    const { t } = useTranslation();
    const [list, setList] = useState<ManagedTasksData>();
    const [before, setBefore] = useState('');
    const [revision, setRevision] = useState(0);
    const [error, setError] = useState<unknown>();
    const [reason, setReason] = useState('');
    const [busy, setBusy] = useState(false);
    const [selected, setSelected] = useState<ManagedTaskData | null>();
    const [completionIdx, setCompletionIdx] = useState('');
    const pendingKey = `dexwallet:task-draft:${session.credential?.idx}`;
    const [pending, setPending] = useState<SaveTaskDraftRequest | undefined>(() => readPending(pendingKey));
    useEffect(() => { const c = new AbortController(); setError(undefined); setList(undefined); void client.request(`/tasks?${new URLSearchParams({ before, limit: '20' })}`, decodeData<ManagedTasksData>, { signal: c.signal }).then(d => { if (!c.signal.aborted)
        setList(d); }, e => { if (!c.signal.aborted)
        setError(e); }); return () => c.abort(); }, [client, before, revision]);
    async function status(row: ManagedTaskData) { setBusy(true); setError(undefined); try {
        await client.request(`/tasks/${row.idx}/status`, decodeData<ManagedTaskData>, { method: 'POST', body: { expectedRevision: row.revision, status: row.status === ActiveStatus ? 'paused' : 'active', reason } });
        setRevision(n => n + 1);
        setSelected(undefined);
    }
    catch (e) {
        setError(e);
    }
    finally {
        setBusy(false);
    } }
    function remember(value: SaveTaskDraftRequest | undefined) { if (value)
        localStorage.setItem(pendingKey, JSON.stringify(value));
    else
        localStorage.removeItem(pendingKey); setPending(value); }
    return <section className="desktop-page"><h1>{t('boTasks')}</h1><p>{t('opBoundary')}</p>{list && !list.writable && <p>{t('opReadOnly')}</p>}
    <Button disabled={!list?.writable || !!pending} onClick={() => setSelected(null)}>{t('opNew')}</Button><Button disabled={busy} onClick={() => setRevision(n => n + 1)}>{t('opRefresh')}</Button>
    <label>{t('boReason')}<Input value={reason} maxLength={512} onChange={e => setReason(e.target.value)}/></label><p>{t('opReasonRequired')}</p>
    <ul>{list?.items.map(row => <li key={row.idx}><strong>{row.title}</strong> · {t(`opTask_${row.status}`)} · {t('opRevision')} {row.revision} · {row.basePoints}
      <Button disabled={!!pending} onClick={() => setSelected(row)}>{t(row.status === DraftStatus ? 'opEdit' : 'opPreview')}</Button>
      <Button disabled={busy || !reason.trim() || (row.status !== ActiveStatus && !list.writable)} onClick={() => { void status(row); }}>{t(row.status === ActiveStatus ? 'opPause' : 'opPublish')}</Button>
      <Button onClick={() => setCompletionIdx(row.idx)}>{t('opCompletions')}</Button></li>)}</ul>
    {list?.items.length === 0 && <p>{t('opEmpty')}</p>}{error !== undefined && <Failure error={error}/>}
    {list?.nextCursor && <Button onClick={() => setBefore(list.nextCursor!)}>{t('boNext')}</Button>}{before && <Button onClick={() => setBefore('')}>{t('boRewardFirst')}</Button>}
    {(pending || selected !== undefined) && <DraftEditor key={`draft:${pending?.idx || selected?.idx || 'new'}`} row={selected ?? undefined} pending={pending} remember={remember} writable={!!list?.writable} onSaved={() => { setSelected(undefined); setRevision(n => n + 1); }} onClose={() => setSelected(undefined)}/>}
    {completionIdx && <Completions key={`completions:${completionIdx}`} idx={completionIdx}/>}
  </section>;
}
function readPending(key: string): SaveTaskDraftRequest | undefined {
    const raw = localStorage.getItem(key);
    if (!raw)
        return;
    try {
        const v = JSON.parse(raw) as Partial<SaveTaskDraftRequest>;
        if (typeof v.requestKey === 'string' && typeof v.title === 'string' && typeof v.description === 'string' && typeof v.basePoints === 'string' && typeof v.channelId === 'string' && typeof v.startsAt === 'string' && typeof v.endsAt === 'string' && typeof v.reason === 'string' && typeof v.expectedRevision === 'number' && (v.idx === undefined || typeof v.idx === 'string'))
            return v as SaveTaskDraftRequest;
    }
    catch { /* Invalid local drafts are not sent. */ }
}
function DraftEditor({ row, pending, remember, writable, onSaved, onClose }: {
    row: ManagedTaskData | undefined;
    pending: SaveTaskDraftRequest | undefined;
    remember: (r: SaveTaskDraftRequest | undefined) => void;
    writable: boolean;
    onSaved: () => void;
    onClose: () => void;
}) {
    const { client } = useAuth();
    const { t } = useTranslation();
    const [title, setTitle] = useState(pending?.title ?? row?.title ?? '');
    const [description, setDescription] = useState(pending?.description ?? row?.description ?? '');
    const [points, setPoints] = useState(String(pending?.basePoints ?? row?.basePoints ?? 100));
    const [channel, setChannel] = useState(pending?.channelId ?? row?.channelId ?? '-100');
    const [start, setStart] = useState((pending?.startsAt ?? row?.startsAt ?? new Date().toISOString()).slice(0, 19));
    const [end, setEnd] = useState((pending?.endsAt ?? row?.endsAt ?? new Date(Date.now() + 86400000 * 30).toISOString()).slice(0, 19));
    const [reason, setReason] = useState(pending?.reason ?? '');
    const [error, setError] = useState<unknown>();
    const [busy, setBusy] = useState(false);
    const immutable = !!row && row.status !== DraftStatus;
    const disabled = busy || !!pending || immutable || !writable;
    async function save() {
        setBusy(true);
        setError(undefined);
        try {
            const body = pending ?? { idx: row?.idx ?? '', expectedRevision: row?.revision ?? 0, requestKey: randomToken(), title, description, basePoints: points, channelId: channel, startsAt: new Date(`${start}Z`).toISOString(), endsAt: new Date(`${end}Z`).toISOString(), reason };
            remember(body);
            await client.request('/task-drafts', decodeData<ManagedTaskData>, { method: 'POST', body });
            remember(undefined);
            onSaved();
        }
        catch (e) {
            if (e instanceof ApiError && ['INVALID_ARGUMENT', 'CONFLICT', 'PERMISSION_DENIED', 'TASK_UNAVAILABLE', 'NOT_FOUND'].includes(e.code ?? ''))
                remember(undefined);
            setError(e);
        }
        finally {
            setBusy(false);
        }
    }
    return <section className="bo-form"><h2>{t(immutable ? 'opPreview' : 'opEdit')}</h2><form onSubmit={e => { e.preventDefault(); void save(); }}>
    <label>{t('opTitle')}<Input required value={title} maxLength={128} disabled={disabled} onChange={e => setTitle(e.target.value)}/></label>
    <label>{t('opDescription')}<Input.TextArea value={description} maxLength={2048} disabled={disabled} onChange={e => setDescription(e.target.value)}/></label>
    <label>{t('opPoints')}<Input inputMode="numeric" pattern="[1-9][0-9]*" required value={points} disabled={disabled} onChange={e => setPoints(e.target.value)}/></label>
    <label>{t('opChannel')}<Input required pattern="-[1-9][0-9]*" value={channel} disabled={disabled} onChange={e => setChannel(e.target.value)}/></label>
    <label>{t('opStart')}<input required type="datetime-local" step="1" value={start} disabled={disabled} onChange={e => setStart(e.target.value)}/></label><label>{t('opEnd')}<input required type="datetime-local" step="1" value={end} min={start} disabled={disabled} onChange={e => setEnd(e.target.value)}/></label>
    {!immutable && <label>{t('boReason')}<Input required value={reason} maxLength={512} disabled={disabled} onChange={e => setReason(e.target.value)}/></label>}
    <article><h3>{t('opPreview')}: {title}</h3><p style={{ whiteSpace: 'pre-wrap' }}>{description}</p><p>{t('opPoints')}: {points}</p><p>{t('opPreviewRule')}</p></article>
    {!immutable && (pending ? <><p>{t('opPending')}</p><Button htmlType="submit" disabled={busy}>{t('opRetry')}</Button></> : <Button htmlType="submit" disabled={disabled || !reason.trim()}>{t('opSave')}</Button>)}
  </form>{error !== undefined && <Failure error={error}/>}<Button disabled={busy || !!pending} onClick={onClose}>{t('opClose')}</Button></section>;
}
function Completions({ idx }: {
    idx: string;
}) {
    const { client } = useAuth();
    const { t } = useTranslation();
    const [before, setBefore] = useState('');
    const [data, setData] = useState<ManagedCompletionsData>();
    const [error, setError] = useState<unknown>();
    const [revision, setRevision] = useState(0);
    useEffect(() => { const c = new AbortController(); setData(undefined); setError(undefined); void client.request(`/tasks/${idx}/completions?${new URLSearchParams({ before, limit: '20' })}`, decodeData<ManagedCompletionsData>, { signal: c.signal }).then(d => { if (!c.signal.aborted)
        setData(d); }, e => { if (!c.signal.aborted)
        setError(e); }); return () => c.abort(); }, [client, idx, before, revision]);
    return <section><h2>{t('opCompletions')}</h2><p>{idx}</p><ul>{data?.items.map(r => <li key={r.idx}>{r.taskTitle} · {r.userIdx} · {t(r.mode === 'self' ? 'opSelf' : 'opHelp')} · {t('opRevision')} {r.taskRevision} · {new Date(r.completedAt).toLocaleString()}</li>)}</ul>{data?.items.length === 0 && <p>{t('opEmpty')}</p>}{error !== undefined && <Failure error={error}/>}<Button onClick={() => setRevision(n => n + 1)}>{t('opRefresh')}</Button>{data?.nextCursor && <Button onClick={() => setBefore(data.nextCursor!)}>{t('boNext')}</Button>}{before && <Button onClick={() => setBefore('')}>{t('boRewardFirst')}</Button>}</section>;
}
