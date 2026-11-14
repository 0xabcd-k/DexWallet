import { describe, expect, it } from 'vitest';
import { decodeRewardTransfer, usdCents } from '../../apps/user/src/api/reward-transfers';
import { readRewardTransfer, rewardTransferStoragePrefix } from '../../apps/user/src/view/reward-transfers/pending';
const result={idx:'03333333333333333',userIdx:'01234567890123456',amount:'2.01',fee:'0.00',status:'completed',reviewReason:'',createdAt:'2026-11-15T00:00:00Z',resolvedAt:'2026-11-15T00:00:00Z'};
describe('reward transfer money and recovery',()=>{
 it('preserves exact cents above floating-point precision',()=>{expect(usdCents('999999999999999999.99')).toBe(99999999999999999999n);expect(usdCents('2.01')).toBe(201n);for(const v of ['-2','2e3','2.001','NaN',' 2','02'])expect(usdCents(v)).toBeUndefined();});
 it('only accepts real transfer outcomes',()=>{expect(decodeRewardTransfer({data:result})).toEqual(result);for(const patch of [{amount:'-2'},{amount:'2.001'},{fee:'0.01'},{status:'paid'},{resolvedAt:''},{userIdx:''}])expect(()=>decodeRewardTransfer({data:{...result,...patch}})).toThrow();});
 it('restores a user-scoped amount and idempotency key',()=>{const req={amount:'2.01',requestKey:'reward_original_key1'};const map=new Map([[rewardTransferStoragePrefix+'a',JSON.stringify(req)]]);const storage={getItem:(key:string)=>map.get(key)??null};expect(readRewardTransfer(storage,'a')).toEqual(req);expect(readRewardTransfer(storage,'b')).toBeUndefined();map.set(rewardTransferStoragePrefix+'a','bad');expect(()=>readRewardTransfer(storage,'a')).toThrow();});
});
