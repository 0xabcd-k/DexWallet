import { useEffect, useState } from 'react';
import { Button, Input } from 'antd';
import { decodeData } from '@dexwallet/api-client';
import type { RewardTransferData, RewardTransferListData, RewardTransferAdminData } from '@dexwallet/api-contract/platform';
import { useTranslation } from '@dexwallet/i18n';
import { Failure, useAuth } from './auth';
export function RewardTransfers() {
  const { session } = useAuth(); return <Queue key={session.generation} />;
}
function Queue() {
  const { client } = useAuth(); const { t } = useTranslation();
  const [status,setStatus]=useState('pending_review'); const [before,setBefore]=useState(''); const [revision,setRevision]=useState(0);
  const [list,setList]=useState<RewardTransferListData>(); const [error,setError]=useState<unknown>(); const [selected,setSelected]=useState('');
  useEffect(()=>{const c=new AbortController();setError(undefined);setList(undefined);
    void client.request(`/reward-transfers?${new URLSearchParams({status,before,limit:'20'})}`,decodeData<RewardTransferListData>,{signal:c.signal}).then(d=>{if(!c.signal.aborted)setList(d);},e=>{if(!c.signal.aborted)setError(e);});return()=>c.abort();
  },[client,status,before,revision]);
  return <section className="desktop-page"><h1>{t('boRewardTransfers')}</h1><p>{t('boRewardRiskBoundary')}</p>
    <label>{t('boStatus')}<select value={status} onChange={e=>{setStatus(e.target.value);setBefore('');setSelected('');}}>{['pending_review','completed','rejected','all'].map(s=><option key={s} value={s}>{t(`rt_${s}`)}</option>)}</select></label>
    <Button onClick={()=>setRevision(n=>n+1)}>{t('boRewardRefresh')}</Button>
    <ul>{list?.items.map(r=><li key={r.idx}><Summary row={r}/><Button onClick={()=>setSelected(r.idx)}>{t('boRewardOpen')}</Button></li>)}</ul>
    {list?.items.length===0&&<p>{t('boRewardEmpty')}</p>}
    {error!==undefined&&<Failure error={error}/>}
    {list?.nextCursor&&<Button onClick={()=>setBefore(list.nextCursor!)}>{t('boNext')}</Button>}{before&&<Button onClick={()=>setBefore('')}>{t('boRewardFirst')}</Button>}
    {selected&&<Review key={selected} idx={selected} onChange={()=>setRevision(n=>n+1)}/>}
  </section>;
}
function Summary({row}:{row:RewardTransferData}) {const {t}=useTranslation();return <p>{row.idx} · {t('boRewardUser')}: {row.userIdx} · ${row.amount} USD · {t(`rt_${row.status}`)}<br/>{t('boRewardFee')}<br/>{row.reviewReason}</p>;}
function Review({idx,onChange}:{idx:string;onChange:()=>void}) {
  const {client}=useAuth();const {t}=useTranslation();const [data,setData]=useState<RewardTransferAdminData>();const [reason,setReason]=useState('');
  const [error,setError]=useState<unknown>();const [busy,setBusy]=useState(false);const [revision,setRevision]=useState(0);
  const [attempt,setAttempt]=useState<{decision:'approve'|'reject';reason:string}>();
  useEffect(()=>{const c=new AbortController();setBusy(true);setError(undefined);
    void client.request(`/reward-transfers/${idx}`,decodeData<RewardTransferAdminData>,{signal:c.signal}).then(d=>{if(!c.signal.aborted){setData(d);if(d.transfer.status!=='pending_review')setAttempt(undefined);}},e=>{if(!c.signal.aborted)setError(e);}).finally(()=>{if(!c.signal.aborted)setBusy(false);});return()=>c.abort();
  },[client,idx,revision]);
  async function review(decision:'approve'|'reject') {
    const body=attempt??{decision,reason:reason.trim()};setAttempt(body);setBusy(true);setError(undefined);
    try {const d=await client.request(`/reward-transfers/${idx}/review`,decodeData<RewardTransferAdminData>,{method:'POST',body});setData(d);setAttempt(undefined);onChange();}
    catch(e){setError(e);}finally{setBusy(false);}
  }
  return <section className="bo-form"><h2>{t('boRewardReview')}</h2>{data&&<><Summary row={data.transfer}/>
    <p>{t('boRewardRisk')}: {data.riskDecision} · {data.riskVersion}<br/>{data.riskReason}</p>
    <p>{t('boRewardReviewer')}: {data.reviewerIdx||'—'}</p>
    {data.transfer.status==='pending_review'&&<><p>{t('boRewardEffects')}</p><label>{t('boReason')}<Input.TextArea value={reason} onChange={e=>setReason(e.target.value)} maxLength={512} disabled={busy||!!attempt}/></label>
      {attempt?<><p>{t('boRewardUnknown')}</p><Button disabled={busy} onClick={()=>{void review(attempt.decision);}}>{t('boRewardRetry')}</Button></>:<><Button disabled={busy||!reason.trim()} onClick={()=>{void review('approve');}}>{t('boRewardApprove')}</Button><Button danger disabled={busy||!reason.trim()} onClick={()=>{void review('reject');}}>{t('boRewardReject')}</Button></>}
    </>}
  </>}{error!==undefined&&<Failure error={error}/>}<Button disabled={busy} onClick={()=>setRevision(n=>n+1)}>{t('boRewardRefresh')}</Button></section>;
}
