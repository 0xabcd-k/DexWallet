import { describe, expect, it } from 'vitest';
import { decodeDraw } from '../../apps/user/src/api/draws';
import { readDraw, drawStoragePrefix } from '../../apps/user/src/view/draws/pending';
const result = { idx: '03333333333333333', versionIdx: '04444444444444444', versionName: 'candidate_v1', costPoints: '100', prizeCode: 'ordinary_one', rewardUsd: '0.08', symbols: [1,5,6], voucherIdx: '05555555555555555', createdAt: '2026-11-15T00:00:00Z' };
describe('draw settlement boundary', () => {
  it('accepts a settled result and rejects malformed money, cost or prize presentation', () => {
    expect(decodeDraw({ data: result })).toEqual(result);
    for (const patch of [{ rewardUsd: '-1.00' }, { costPoints: '99' }, { symbols: [1,1,1] }, { symbols: [1,8,1] }, { prizeCode: 'unknown' }, { createdAt: 'unknown' }, { voucherIdx: '' }]) {
      expect(() => decodeDraw({ data: { ...result, ...patch } })).toThrow();
    }
  });
  it('restores the original version and key, scoped to the product user', () => {
    const request = { requestKey: 'draw_original_request', versionIdx: result.versionIdx }; const store = new Map([[drawStoragePrefix + 'a', JSON.stringify(request)]]);
    const storage = { getItem: (k: string) => store.get(k) ?? null };
    expect(readDraw(storage, 'a')).toEqual(request); expect(readDraw(storage, 'b')).toBeUndefined();
    store.set(drawStoragePrefix + 'a', JSON.stringify({ ...request, versionIdx: 'wrong' })); expect(() => readDraw(storage, 'a')).toThrow();
  });
});
