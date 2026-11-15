import { useEffect, useState } from 'react';
import { Button, Input } from 'antd';
import { ApiError, decodeData } from '@dexwallet/api-client';
import type { DrawPrizeData, ManagedDrawData, ManagedDrawVersion, SaveDrawVersionRequest } from '@dexwallet/api-contract/platform';
import { useTranslation } from '@dexwallet/i18n';
import { Failure, useAuth } from './auth';
const samplePrizes: DrawPrizeData[] = [
    { code: 'ordinary_one', rewardUsd: '0.08', weight: 1500, symbols: [1, 5, 6] },
    { code: 'ordinary_two', rewardUsd: '0.09', weight: 5000, symbols: [1, 1, 5] },
    { code: 'ordinary_three', rewardUsd: '0.10', weight: 3000, symbols: [1, 1, 1] },
    { code: 'small', rewardUsd: '0.12', weight: 400, symbols: [2, 2, 2] },
    { code: 'double', rewardUsd: '0.20', weight: 90, symbols: [3, 3, 3] },
    { code: 'large', rewardUsd: '1.00', weight: 9, symbols: [4, 4, 4] },
    { code: 'top', rewardUsd: '5.00', weight: 1, symbols: [7, 7, 7] },
];
export function DrawVersions() { const { session } = useAuth(); return <Versions key={session.generation}/>; }
function Versions() {
    const { client } = useAuth();
    const { t } = useTranslation();
    const [data, setData] = useState<ManagedDrawData>();
    const [before, setBefore] = useState('');
    const [revision, setRevision] = useState(0);
    const [reason, setReason] = useState('');
    const [selected, setSelected] = useState<ManagedDrawVersion>();
    const [creating, setCreating] = useState(false);
    const [error, setError] = useState<unknown>();
    const [busy, setBusy] = useState(false);
    useEffect(() => { const c = new AbortController(); setData(undefined); setError(undefined); void client.request(`/draw-versions?${new URLSearchParams({ before, limit: '20' })}`, decodeData<ManagedDrawData>, { signal: c.signal }).then(d => { if (!c.signal.aborted)
        setData(d); }, e => { if (!c.signal.aborted)
        setError(e); }); return () => c.abort(); }, [client, before, revision]);
    async function control(versionIdx: string) { if (!data)
        return; setBusy(true); setError(undefined); try {
        await client.request('/draw-control', decodeData<ManagedDrawData>, { method: 'POST', body: { versionIdx, expectedRevision: data.revision, reason } });
        setRevision(n => n + 1);
    }
    catch (e) {
        setError(e);
    }
    finally {
        setBusy(false);
    } }
    return <section className="desktop-page"><h1>{t('boDrawVersions')}</h1><p>{t('opDrawBoundary')}</p>{data && !data.writable && <p>{t('opReadOnly')}</p>}<p>{t('opCurrent')}: {data?.currentVersionIdx || t('opStopped')} · {t('opRevision')} {data?.revision}</p>
    <Button disabled={!data?.writable || busy} onClick={() => setCreating(true)}>{t('opNewVersion')}</Button><Button disabled={busy} onClick={() => setRevision(n => n + 1)}>{t('opRefresh')}</Button>
    <label>{t('boReason')}<Input value={reason} onChange={e => setReason(e.target.value)} maxLength={512}/></label><p>{t('opReasonRequired')}</p><Button disabled={!data?.currentVersionIdx || busy || !reason.trim()} onClick={() => { void control(''); }}>{t('opPause')}</Button>
    <ul>{data?.items.map(v => <li key={v.idx}><strong>{v.name}</strong> · {v.rtp} · {v.idx}<Button onClick={() => setSelected(v)}>{t('opPreview')}</Button><Button disabled={!data.writable || busy || !reason.trim() || data.currentVersionIdx === v.idx} onClick={() => { void control(v.idx); }}>{t('opActivate')}</Button></li>)}</ul>
    {data?.items.length === 0 && <p>{t('opEmpty')}</p>}{error !== undefined && <Failure error={error}/>}{data?.nextCursor && <Button onClick={() => setBefore(data.nextCursor!)}>{t('boNext')}</Button>}{before && <Button onClick={() => setBefore('')}>{t('boRewardFirst')}</Button>}
    {selected && <section><h2>{selected.name}</h2><PrizeTable prizes={selected.prizes}/></section>}
    {creating && <NewVersion onSaved={v => { setSelected(v); setCreating(false); setRevision(n => n + 1); }} onClose={() => setCreating(false)}/>}
  </section>;
}
function PrizeTable({ prizes, onChange, disabled }: {
    prizes: DrawPrizeData[];
    onChange?: (value: DrawPrizeData[]) => void;
    disabled?: boolean;
}) { const { t } = useTranslation(); return <table><thead><tr>{['opPrize', 'opAmount', 'opWeight', 'opSymbols'].map(k => <th key={k}>{t(k)}</th>)}</tr></thead><tbody>{prizes.map((p, i) => <tr key={p.code}><td>{t(`opPrize_${p.code}`)}</td><td>{onChange ? <Input aria-label={`${t(`opPrize_${p.code}`)} ${t('opAmount')}`} required pattern="[0-9]+([.][0-9]{1,2})?" value={p.rewardUsd} disabled={!!disabled} onChange={e => onChange(prizes.map((x, j) => j === i ? { ...x, rewardUsd: e.target.value } : x))}/> : p.rewardUsd}</td><td>{onChange ? <Input aria-label={`${t(`opPrize_${p.code}`)} ${t('opWeight')}`} required type="number" min="0" max="10000" step="1" value={p.weight} disabled={!!disabled} onChange={e => onChange(prizes.map((x, j) => j === i ? { ...x, weight: Number(e.target.value) } : x))}/> : p.weight}</td><td>{p.symbols.join(' · ')}</td></tr>)}</tbody></table>; }
function NewVersion({ onSaved, onClose }: {
    onSaved: (v: ManagedDrawVersion) => void;
    onClose: () => void;
}) {
    const { client } = useAuth();
    const { t } = useTranslation();
    const [name, setName] = useState('');
    const [rtp, setRtp] = useState('0.95');
    const [prizes, setPrizes] = useState(samplePrizes);
    const [reason, setReason] = useState('');
    const [attempt, setAttempt] = useState<SaveDrawVersionRequest>();
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<unknown>();
    async function save() { const body = attempt ?? { name, rtp, prizes, reason }; setAttempt(body); setBusy(true); setError(undefined); try {
        const v = await client.request('/draw-versions', decodeData<ManagedDrawVersion>, { method: 'POST', body });
        setAttempt(undefined);
        onSaved(v);
    }
    catch (e) {
        if (e instanceof ApiError && ['INVALID_ARGUMENT', 'CONFLICT', 'DRAW_UNAVAILABLE', 'PERMISSION_DENIED'].includes(e.code ?? ''))
            setAttempt(undefined);
        setError(e);
    }
    finally {
        setBusy(false);
    } }
    const disabled = busy || !!attempt;
    return <section className="bo-form"><h2>{t('opNewVersion')}</h2><p>{t('opVersionRule')}</p><form onSubmit={e => { e.preventDefault(); void save(); }}><label>{t('opName')}<Input required pattern="[a-zA-Z0-9_-]{1,64}" value={name} disabled={!!disabled} onChange={e => setName(e.target.value)}/></label><label>{t('opRtp')}<Input required pattern="[0-9]+([.][0-9]+)?" value={rtp} disabled={!!disabled} onChange={e => setRtp(e.target.value)}/></label><PrizeTable prizes={prizes} onChange={setPrizes} disabled={!!disabled}/><label>{t('boReason')}<Input required value={reason} maxLength={512} disabled={!!disabled} onChange={e => setReason(e.target.value)}/></label>{attempt && <p>{t('opPendingDraw')}</p>}<Button htmlType="submit" disabled={busy || !reason.trim()}>{t(attempt ? 'opRetry' : 'opSave')}</Button></form>{error !== undefined && <Failure error={error}/>}<Button disabled={busy || !!attempt} onClick={onClose}>{t('opClose')}</Button></section>;
}
