import { decodeData, isRecord, type Decoder } from '@dexwallet/api-client';
import type { DrawRequest, DrawStateData, DrawResultData, DrawListData } from '@dexwallet/api-contract/platform';
import { userRequest } from './account';
const validIdx = (v: unknown) => typeof v === 'string' && /^0\d{16,19}$/.test(v);
const prizeSymbols: Record<string, number> = { small: 2, double: 3, large: 4, top: 7 };
export function validDrawResult(d: unknown): boolean {
  if (!isRecord(d) || !validIdx(d.idx) || !validIdx(d.versionIdx) || !validIdx(d.voucherIdx) || d.costPoints !== '100' ||
    typeof d.versionName !== 'string' || typeof d.rewardUsd !== 'string' || !/^(0|[1-9]\d*)\.\d{2}$/.test(d.rewardUsd) || d.rewardUsd === '0.00' ||
    typeof d.createdAt !== 'string' || !Number.isFinite(Date.parse(d.createdAt)) || typeof d.prizeCode !== 'string' ||
    !Array.isArray(d.symbols) || d.symbols.length !== 3 || !d.symbols.every(s => Number.isInteger(s) && s >= 1 && s <= 7)) return false;
  const ones = d.symbols.filter(s => s === 1).length;
  const ordinary = ['ordinary_one', 'ordinary_two', 'ordinary_three'][ones - 1];
  const code = d.prizeCode;
  return ones ? code === ordinary : Object.hasOwn(prizeSymbols, code) && d.symbols.every(s => s === prizeSymbols[code]);
}
export const decodeDraw: Decoder<DrawResultData> = value => {
  if (!isRecord(value) || !validDrawResult(value.data)) throw new TypeError('Invalid draw result');
  return decodeData<DrawResultData>(value);
};
export const getDrawState = (signal: AbortSignal) => userRequest('/v1/user/draw-state', decodeData<DrawStateData>, { signal });
export const createDraw = (request: DrawRequest, signal: AbortSignal) => userRequest('/v1/user/draws', decodeDraw, { method: 'POST', body: request, signal });
export const drawByRequest = (key: string, signal: AbortSignal) => userRequest(`/v1/user/draws/by-request/${encodeURIComponent(key)}`, decodeDraw, { signal });
export const getDraw = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/draws/${encodeURIComponent(idx)}`, decodeDraw, { signal });
export const getDraws = (before: string, signal: AbortSignal) => userRequest(`/v1/user/draws?${new URLSearchParams({ before, limit: '20' })}`, decodeData<DrawListData>, { signal });
