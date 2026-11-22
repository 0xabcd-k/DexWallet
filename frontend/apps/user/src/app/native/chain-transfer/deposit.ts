import { decodeData } from '@dexwallet/api-client';
import type { ChainAsset, DepositQuoteData } from '@dexwallet/api-contract/platform';
import { userRequest } from '../../../api/account';
import { atomicAmount } from './api';

export const validDepositTarget = (value: string) => /^(0|[1-9]\d{0,17})(\.\d{1,2})?$/.test(value) && (atomicAmount(value, 2) ?? 0n) > 0n;

export function previewDeposit(asset: ChainAsset, native: ChainAsset, target: string, signal: AbortSignal) {
  return userRequest('/v1/user/deposit-quotes/preview', value => {
    const q = decodeData<DepositQuoteData>(value);
    const units = (v: unknown, scale: number) => typeof v === 'string' ? atomicAmount(v, scale) : undefined;
    const targetCents = units(target, 2), net = units(q.netUsd, 2);
    if (q.assetId !== asset.id || q.gasAssetId !== native.id || !targetCents || units(q.targetUsd, 2) !== targetCents ||
      net === undefined || net < targetCents || (units(q.payQuantity, asset.decimals) ?? 0n) <= 0n || (units(q.usdPerCoin, 18) ?? 0n) <= 0n ||
      units(q.estimatedGas, native.decimals) === undefined || units(q.gasUsd, native.decimals + 18) === undefined ||
      typeof q.rateVersion !== 'string' || !q.rateVersion || !Number.isSafeInteger(q.asOf) || !Number.isSafeInteger(q.quotedAt) || !Number.isSafeInteger(q.expiresAt) ||
      q.asOf <= 0 || q.quotedAt < q.asOf || q.expiresAt <= q.quotedAt || q.canSubmit !== false) throw new TypeError('Invalid test deposit estimate');
    return q;
  }, { method: 'POST', body: { assetId: asset.id, targetUsd: target }, signal });
}
