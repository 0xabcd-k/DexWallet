import { decodeData } from '@dexwallet/api-client';
import type { DepositQuoteData, BaseDepositOrderData } from '@dexwallet/api-contract/platform';
import { secp256k1 } from '@noble/curves/secp256k1.js';
import { keccak_256 } from '@noble/hashes/sha3.js';
import { bytesToHex, concatBytes, hexToBytes } from '@noble/hashes/utils.js';
import { HDKey } from '@scure/bip32';
import { userRequest } from '../../../api/account';
import { DERIVATION, WalletError } from '../local-wallet/crypto';
import { signWithSeed } from './sign-with-seed';
import { atomicAmount } from './api';

const contract = '0x036CbD53842c5426634e7929541eC2318f3dCF7e';
const idFormat = /^[0-9a-f]{32}$/;
const orders = '/v1/user/base-deposit-orders/';
const hashText = (s: string) => keccak_256(new TextEncoder().encode(s));
const word = (hex: string) => hexToBytes(hex.padStart(64, '0'));
const ethAsset = 'base-sepolia:eth';
const domainType = hashText('EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)');
function domain(name: string, version: string, verifyingContract: string) {
  return keccak_256(concatBytes(domainType, hashText(name), hashText(version), word((84532).toString(16)), word(verifyingContract.slice(2))));
}
const usdcDomain = domain('USDC', '2', contract);
const ethDomain = domain('DelegationManager', '1', '0xdb9B1e94B5b69Df7e401DDbedE43491141047dB3');
const caveatType = 'Caveat(address enforcer,bytes terms)';
const grantType = hashText('Delegation(address delegate,address delegator,bytes32 authority,Caveat[] caveats,uint256 salt)' + caveatType);
const enforcers = {
  periodAllowance: '9BC0FAf4Aca5AE429F4c06aEEaC517520CB16BD9',
  exactCalldata: '99F2e9bF15ce5eC84685604836F71aB835DBBdED',
  timestamp: '1046bb45C8d673d4ea75321280DB34899413c069',
  allowedRedeemer: 'E144b0b2618071B4E56f746313528a669c7E65c5',
  allowedTarget: '7F20f61b1f09b08D970938F6fa563634d65c4EeB',
  revocationNonce: 'DE4f2FAC4B3D87A1d9953Ca5FC09FCa7F366254f',
} as const;
function ethGrant(owner: string, payer: string, amount: bigint, start: number, expiry: number, salt: Uint8Array) {
  const unlimited = word('ff'.repeat(32));
  const addressBytes = hexToBytes(payer.slice(2));
  const caveat = (enforcer: string, terms: Uint8Array) =>
    keccak_256(concatBytes(hashText(caveatType), word(enforcer), keccak_256(terms)));
  const caveats = keccak_256(concatBytes(
    caveat(enforcers.periodAllowance, concatBytes(word(amount.toString(16)), unlimited, word(start.toString(16)))),
    caveat(enforcers.exactCalldata, new Uint8Array()),
    caveat(enforcers.timestamp, word(expiry.toString(16))),
    caveat(enforcers.allowedRedeemer, addressBytes),
    caveat(enforcers.allowedTarget, addressBytes),
    caveat(enforcers.revocationNonce, word('0')),
  ));
  return keccak_256(concatBytes(grantType, word(payer.slice(2)), word(owner.slice(2)), unlimited, caveats, salt));
}
const transferType = hashText('TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)');
const noncePrefix = hashText('dexwallet:deposit:base-sepolia:').slice(0, 16);
const address = (v: unknown): v is string => typeof v === 'string' && /^0x[0-9a-fA-F]{40}$/.test(v) && !/^0x0{40}$/.test(v);
export const lastBaseQuoteKey = (userIdx: string) => 'dexwallet:base-deposit:last:' + userIdx;

export function baseDepositDigest(q: DepositQuoteData) {
  const isETH = q.assetId === ethAsset;
  if ((!isETH && q.assetId !== 'base-sepolia:usdc') || q.gasAssetId !== ethAsset || !q.walletFeePreview ||
    !q.quoteIdx || !idFormat.test(q.quoteIdx) || !address(q.walletAddress) || !address(q.baseRecipient) || !address(q.baseFeePayer) ||
    [q.baseRecipient,q.baseFeePayer].some(addr => addr.toLowerCase() === q.walletAddress!.toLowerCase()) ||
    !Number.isSafeInteger(q.baseValidBefore) || q.baseValidBefore! <= Math.floor(q.expiresAt / 1000) ||
    !q.maxGas || q.maxGas !== q.estimatedGas || (atomicAmount(q.maxGas, 18) ?? 0n) <= 0n) throw new TypeError('Invalid Base signing terms');
  const atoms = atomicAmount(q.payQuantity, isETH ? 18 : 6);
  if (!atoms || atoms >= 1n << 256n) throw new TypeError('Invalid Base deposit quantity');
  const salt = concatBytes(noncePrefix, hexToBytes(q.quoteIdx));
  if (isETH && (q.baseRecipient.toLowerCase() !== q.baseFeePayer.toLowerCase() || !Number.isSafeInteger(q.quotedAt) || q.quotedAt <= 0 || q.quotedAt >= q.expiresAt)) throw new TypeError('Invalid native ETH permission');
  const message = isETH ? ethGrant(q.walletAddress, q.baseFeePayer, atoms, Math.floor(q.quotedAt / 1000), q.baseValidBefore!, salt)
    : keccak_256(concatBytes(transferType, word(q.walletAddress.slice(2)), word(q.baseRecipient.slice(2)),
    word(atoms.toString(16)), word('0'), word(q.baseValidBefore!.toString(16)), salt));
  return keccak_256(concatBytes(Uint8Array.of(25, 1), isETH ? ethDomain : usdcDomain, message));
}

function orderData(value: unknown, idx: string) {
  const o = decodeData<BaseDepositOrderData>(value);
  // Servers before business 44 returned only USDC orders.
  const assetId = o.assetId === undefined ? 'base-sepolia:usdc' : o.assetId;
  if (![ethAsset, 'base-sepolia:usdc'].includes(assetId) || o.quoteIdx !== idx || !['awaiting_signature','expired','accepted','signed','review','credited'].includes(o.state) ||
    (o.txHash !== undefined && !/^0x[a-f0-9]{64}$/.test(o.txHash)) ||
    (['signed','credited'].includes(o.state) && !o.txHash) ||
    (['awaiting_signature','expired','accepted'].includes(o.state) && o.txHash !== undefined) ||
    (o.actualFeeWei !== undefined && !/^(0|[1-9]\d{0,77})$/.test(o.actualFeeWei)) ||
    (o.state === 'credited' ? !o.actualFeeWei || !o.creditedUsd || !/^\d+\.\d{2}$/.test(o.creditedUsd) || o.creditedUsd === '0.00' : o.creditedUsd !== undefined)) {
    throw new TypeError('Invalid Base deposit order status');
  }
  return { ...o, assetId };
}
export function getBaseOrder(idx: string, signal: AbortSignal) {
  if (!idFormat.test(idx)) throw new TypeError('Invalid Base order ID');
  return userRequest(orders + idx, v => orderData(v, idx), { signal });
}
export async function authorizeBaseDeposit(userIdx: string, walletId: string, q: DepositQuoteData, deadline: number, signal: AbortSignal) {
  const digest = baseDepositDigest(q), idx = q.quoteIdx!;
  const original = await getBaseOrder(idx, signal);
  if (original.assetId !== q.assetId) throw new TypeError('Deposit order asset differs from quote');
  if (original.state !== 'awaiting_signature') return original;
  const check = () => { signal.throwIfAborted(); if (Date.now() >= deadline) throw new Error('Quote expired'); };
  check();
  const signature = await signWithSeed(userIdx, walletId, signal, seed => {
    const root = HDKey.fromMasterSeed(seed), key = root.derive(DERIVATION.base);
    try {
      check();
      if (!key.privateKey) throw new WalletError('LOCAL_DERIVATION_FAILED');
      const pub = secp256k1.getPublicKey(key.privateKey, false);
      if ('0x' + bytesToHex(keccak_256(pub.slice(1)).slice(-20)) !== q.walletAddress!.toLowerCase()) throw new WalletError('LOCAL_VIEW_CLOSED');
      const sig = secp256k1.sign(digest, key.privateKey, { prehash: false, format: 'recovered' });
      return '0x' + bytesToHex(concatBytes(sig.slice(1), Uint8Array.of(sig[0]! + 27)));
    } finally { root.wipePrivateData(); key.wipePrivateData(); }
  });
  check();
  const accepted = await userRequest(orders + 'authorize', v => orderData(v, idx), { method: 'POST', body: { quoteIdx: idx, userSignature: signature }, signal });
  if (accepted.assetId !== q.assetId || ['awaiting_signature','expired'].includes(accepted.state)) throw new TypeError('Missing Base deposit authorization');
  return accepted;
}
