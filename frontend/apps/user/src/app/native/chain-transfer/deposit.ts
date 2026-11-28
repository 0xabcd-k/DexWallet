import { decodeData } from '@dexwallet/api-client';
import type { ChainAsset, ConfirmedDepositQuoteData, DepositQuoteData } from '@dexwallet/api-contract/platform';
import { userRequest } from '../../../api/account';
import { depositTokenMint, solanaDepositSOL, solanaDepositUSDC, solanaTestUSDTPrefix } from './sol-deposit-asset';
import { nileDepositUSDT } from './tron-deposit-asset';
import { atomicAmount } from './api';

import { testJettonAsset } from './ton-deposit-asset';

const depositQuoteTTLms = 30_000;

export const validDepositTarget = (value: string) => /^(0|[1-9]\d{0,17})(\.\d{1,2})?$/.test(value) && (atomicAmount(value, 2) ?? 0n) > 0n;

export function previewDeposit(asset: ChainAsset, native: ChainAsset, target: string, signal: AbortSignal, walletAddress?: string, beneficiaryIdx?: string) {
  return userRequest('/v1/user/deposit-quotes/preview', value => {
    const q = decodeData<DepositQuoteData>(value);
    const units = (v: unknown, scale: number) => typeof v === 'string' ? atomicAmount(v, scale) : undefined;
    const targetCents = units(target, 2), net = units(q.netUsd, 2);
    const baseFeeBlock = native.id === 'base-sepolia:eth' && (asset.id === native.id || asset.id === 'base-sepolia:usdc');
    const tonNetwork = native.id === 'ton-testnet:ton';
    const tonJetton = tonNetwork && asset.origin === 'test_fixture' && !!asset.contract && asset.id === 'ton-testnet:usdt:' + asset.contract;
    const tonRelay = !!walletAddress && tonNetwork && (asset.id === native.id || tonJetton);
    const validExcess = tonJetton && walletAddress ? (units(q.jettonExcess, native.decimals) ?? 0n) > 0n : q.jettonExcess === undefined;
    const attachment = units(q.relayAttached, native.decimals), surplus = units(q.relaySurplus, native.decimals);
    const validRelay = tonRelay
      ? typeof q.emulatedMessageHash === 'string' && /^[a-f0-9]{64}$/.test(q.emulatedMessageHash) &&
        attachment !== undefined && attachment > 0n && surplus !== undefined && surplus <= attachment && (units(q.estimatedGas, native.decimals) ?? 0n) > 0n
      : q.emulatedMessageHash === undefined && q.relayAttached === undefined && q.relaySurplus === undefined;
    const tronTRX = native.id === 'tron-nile:trx' && asset.id === native.id;
    const tronToken = native.id === 'tron-nile:trx' && asset.origin === 'test_fixture' && !!asset.contract && asset.id === 'tron-nile:usdt:' + asset.contract;
    const needsBandwidth = (tronTRX || tronToken) && !!walletAddress;
    const validEnergy = tronToken && walletAddress
      ? typeof q.delegatedEnergy === 'number' && Number.isSafeInteger(q.delegatedEnergy) && q.delegatedEnergy > 0
      : q.delegatedEnergy === undefined;
    const validBandwidth = needsBandwidth
      ? typeof q.delegatedBandwidth === 'number' && Number.isSafeInteger(q.delegatedBandwidth) && q.delegatedBandwidth > 0 && units(q.estimatedGas, native.decimals) === 0n && units(q.gasUsd, native.decimals + 18) === 0n
      : q.delegatedBandwidth === undefined;
    const walletEvidence = tonRelay ? !q.feeBlockHash && !q.messageSha256 : baseFeeBlock
      ? typeof q.feeBlockHash === 'string' && /^0x[a-f0-9]{64}$/.test(q.feeBlockHash) && !q.messageSha256
      : (tronTRX || tronToken || native.id === 'solana-devnet:sol') && typeof q.messageSha256 === 'string' && /^[a-f0-9]{64}$/.test(q.messageSha256) && !q.feeBlockHash;
    const validSource = walletAddress
      ? q.walletAddress === walletAddress && q.walletFeePreview === true && walletEvidence
      : !q.walletAddress && !q.messageSha256 && !q.feeBlockHash;
    const tokenWallet = walletAddress && (tonJetton || (native.id === 'solana-devnet:sol' && asset.contract !== ''));
    const funding = units(q.accountFunding, native.decimals), fundingUsd = units(q.accountFundingUsd, native.decimals + 18);
    const validFunding = tokenWallet
      ? funding !== undefined && fundingUsd !== undefined && (funding === 0n) === (fundingUsd === 0n)
      : !q.accountFunding && !q.accountFundingUsd;
    const validOffer = q.quoteIdx === undefined && q.maxGas === undefined ||
      !!walletAddress && typeof q.quoteIdx === 'string' && /^[0-9a-f]{32}$/.test(q.quoteIdx) && q.maxGas === q.estimatedGas;
    const solTerms = q.solOrderIdx === undefined && q.solRecipient === undefined && q.solFeePayer === undefined ||
      (asset.id === solanaDepositSOL || asset.id === solanaDepositUSDC ||
        (asset.origin === 'test_fixture' && asset.decimals === 6 && asset.id === solanaTestUSDTPrefix + asset.contract && !!depositTokenMint(asset.id))) && !!walletAddress && !!q.quoteIdx && typeof q.solOrderIdx === 'string' && /^[a-f0-9]{32}$/.test(q.solOrderIdx) &&
      [q.solFeePayer, q.solRecipient].every(v => typeof v === 'string' && /^[1-9A-HJ-NP-Za-km-z]{32,44}$/.test(v));
    const hasBaseTerms = q.baseValidBefore !== undefined || q.baseRecipient !== undefined || q.baseFeePayer !== undefined;
    const baseAssetMatches = asset.id === 'base-sepolia:usdc'
      ? asset.origin === 'issuer' && asset.contract.toLowerCase() === '0x036cbd53842c5426634e7929541ec2318f3dcf7e' && asset.decimals === 6
      : asset.id === 'base-sepolia:eth' && asset.origin === 'native' && asset.contract === '' && asset.decimals === 18 &&
        typeof q.baseFeePayer === 'string' && q.baseFeePayer.toLowerCase() === q.baseRecipient?.toLowerCase();
    const validBase = !hasBaseTerms || (baseAssetMatches && !!walletAddress && !!q.quoteIdx && Number.isSafeInteger(q.baseValidBefore) && q.baseValidBefore! > Math.floor(q.expiresAt / 1000) &&
      [q.baseRecipient, q.baseFeePayer].every(a => typeof a === 'string' && /^0x[a-fA-F0-9]{40}$/.test(a) && !/^0x0{40}$/.test(a) && a.toLowerCase() !== walletAddress.toLowerCase()));
    const hasTron = [q.tronRefBlock, q.tronRefTime, q.tronRecipient, q.tronResourceOwner].some(v => v !== undefined);
    const validTron = !hasTron || ((tronTRX || (tronToken && asset.decimals === 6 && asset.id === nileDepositUSDT)) && !!walletAddress && !!q.quoteIdx && q.maxGas === '0' &&
      typeof q.tronRefBlock === 'string' && /^[a-f0-9]{64}$/.test(q.tronRefBlock) && Number.isSafeInteger(q.tronRefTime) && q.tronRefTime! > 0 &&
      [q.tronRecipient, q.tronResourceOwner].every(a => typeof a === 'string' && /^T[1-9A-HJ-NP-Za-km-z]{33}$/.test(a) && a !== walletAddress));
    const hasTonTerms = [q.tonValidUntil, q.tonUserSeqno, q.tonRecipient, q.tonAuthorizationHash, q.tonSimulatedGas, q.tonJettonWalletCode, q.tonSenderPaidGas].some(v => v !== undefined);
    const senderPaid = units(q.tonSenderPaidGas, 9);
    const tonSigningAsset = tonJetton ? senderPaid !== undefined && senderPaid <= (units(q.tonSimulatedGas, 9) ?? 0n) && asset.id === testJettonAsset && asset.decimals === 6 && q.relayAttached === '0.053' &&
      typeof q.tonJettonWalletCode === 'string' && /^[0-9a-f]{2,8192}$/.test(q.tonJettonWalletCode) :
      asset.id === native.id && asset.decimals === 9 && q.tonJettonWalletCode === undefined && q.tonSenderPaidGas === undefined && q.relayAttached === '0.003';
    const validTon = !hasTonTerms || (tonRelay && tonSigningAsset && !!q.quoteIdx &&
      Number.isSafeInteger(q.tonUserSeqno) && q.tonUserSeqno! > 0 && q.tonUserSeqno! <= 0xffffffff &&
      Number.isSafeInteger(q.tonValidUntil) && q.tonValidUntil! > q.expiresAt / 1000 && q.tonValidUntil! <= 0xffffffff &&
      typeof q.tonRecipient === 'string' && /^0:[a-f0-9]{64}$/.test(q.tonRecipient) &&
      typeof q.tonAuthorizationHash === 'string' && /^[a-f0-9]{64}$/.test(q.tonAuthorizationHash) &&
      (units(q.tonSimulatedGas, 9) ?? 0n) > 0n && units(q.tonSimulatedGas, 9)! <= (units(q.maxGas, 9) ?? 0n));
    if (q.beneficiaryIdx !== beneficiaryIdx || (beneficiaryIdx !== undefined && (asset.id !== solanaDepositSOL || (!!walletAddress && !q.solOrderIdx))) || !validTon || !validTron || !validBase || !solTerms || !validOffer || !validExcess || !validRelay || !validEnergy || !validBandwidth || !validFunding || !validSource || q.assetId !== asset.id || q.gasAssetId !== native.id || !targetCents || units(q.targetUsd, 2) !== targetCents ||
      net === undefined || net < targetCents || (units(q.payQuantity, asset.decimals) ?? 0n) <= 0n || (units(q.usdPerCoin, 18) ?? 0n) <= 0n ||
      units(q.estimatedGas, native.decimals) === undefined || units(q.gasUsd, native.decimals + 18) === undefined ||
      (q.rateSource !== undefined && q.rateSource !== 'test_sheet' && q.rateSource !== 'kraken') ||
      typeof q.rateVersion !== 'string' || !q.rateVersion || !Number.isSafeInteger(q.asOf) || !Number.isSafeInteger(q.quotedAt) || !Number.isSafeInteger(q.expiresAt) ||
      q.asOf <= 0 || q.quotedAt < q.asOf || q.expiresAt <= q.quotedAt || q.expiresAt - q.quotedAt > depositQuoteTTLms || q.canSubmit !== false) throw new TypeError('Invalid test deposit estimate');
    return q;
  }, {
    method: 'POST', body: { assetId: asset.id, targetUsd: target, walletAddress, beneficiaryIdx }, signal,
    ...(walletAddress ? { timeoutMs: depositQuoteTTLms } : {}),
  });
}

export function confirmDepositQuote(quote: DepositQuoteData, lookupOnly: boolean, signal: AbortSignal) {
  const path = '/v1/user/deposit-quotes/' + (lookupOnly ? quote.quoteIdx : 'confirm');
  return userRequest(path, value => {
    const record = decodeData<ConfirmedDepositQuoteData>(value);
    if (!record.quote || record.quote.beneficiaryIdx !== quote.beneficiaryIdx || !Number.isSafeInteger(record.confirmedAt) || record.confirmedAt < quote.quotedAt || record.confirmedAt >= quote.expiresAt ||
      Object.entries(quote).some(([key, term]) => record.quote[key as keyof DepositQuoteData] !== term)) {
      throw new TypeError('Invalid deposit quote confirmation');
    }
    return record.confirmedAt;
  }, lookupOnly ? { signal } : { method: 'POST', body: { quoteIdx: quote.quoteIdx }, signal });
}
