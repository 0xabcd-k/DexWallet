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
const domain = keccak_256(concatBytes(hashText('EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)'),
  hashText('USDC'), hashText('2'), word((84532).toString(16)), word(contract.slice(2))));
const transferType = hashText('TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)');
const noncePrefix = hashText('dexwallet:deposit:base-sepolia:').slice(0, 16);
const address = (v: unknown): v is string => typeof v === 'string' && /^0x[0-9a-fA-F]{40}$/.test(v) && !/^0x0{40}$/.test(v);
export const lastBaseQuoteKey = (userIdx: string) => 'dexwallet:base-deposit:last:' + userIdx;

export function baseDepositDigest(q: DepositQuoteData) {
  if (q.assetId !== 'base-sepolia:usdc' || q.gasAssetId !== 'base-sepolia:eth' || !q.walletFeePreview ||
    !q.quoteIdx || !idFormat.test(q.quoteIdx) || !address(q.walletAddress) || !address(q.baseRecipient) || !address(q.baseFeePayer) ||
    [q.baseRecipient,q.baseFeePayer].some(addr => addr.toLowerCase() === q.walletAddress!.toLowerCase()) ||
    !Number.isSafeInteger(q.baseValidBefore) || q.baseValidBefore! <= Math.floor(q.expiresAt / 1000) ||
    !q.maxGas || q.maxGas !== q.estimatedGas || (atomicAmount(q.maxGas, 18) ?? 0n) <= 0n) throw new TypeError('Invalid Base USDC signing terms');
  const atoms = atomicAmount(q.payQuantity, 6);
  if (!atoms || atoms >= 1n << 256n) throw new TypeError('Invalid USDC quantity');
  const message = keccak_256(concatBytes(transferType, word(q.walletAddress.slice(2)), word(q.baseRecipient.slice(2)),
    word(atoms.toString(16)), word('0'), word(q.baseValidBefore!.toString(16)), noncePrefix, hexToBytes(q.quoteIdx)));
  return keccak_256(concatBytes(Uint8Array.of(25, 1), domain, message));
}

function orderData(value: unknown, idx: string) {
  const o = decodeData<BaseDepositOrderData>(value);
  if (o.quoteIdx !== idx || !['awaiting_signature','expired','accepted','signed','review','credited'].includes(o.state) ||
    (o.txHash !== undefined && !/^0x[a-f0-9]{64}$/.test(o.txHash)) ||
    (['signed','credited'].includes(o.state) && !o.txHash) ||
    (['awaiting_signature','expired','accepted'].includes(o.state) && o.txHash !== undefined) ||
    (o.actualFeeWei !== undefined && !/^(0|[1-9]\d{0,77})$/.test(o.actualFeeWei)) ||
    (o.state === 'credited' ? !o.actualFeeWei || !o.creditedUsd || !/^\d+\.\d{2}$/.test(o.creditedUsd) || o.creditedUsd === '0.00' : o.creditedUsd !== undefined)) {
    throw new TypeError('Invalid Base USDC order status');
  }
  return o;
}
export function getBaseOrder(idx: string, signal: AbortSignal) {
  if (!idFormat.test(idx)) throw new TypeError('Invalid Base order ID');
  return userRequest(orders + idx, v => orderData(v, idx), { signal });
}
export async function authorizeBaseDeposit(userIdx: string, walletId: string, q: DepositQuoteData, deadline: number, signal: AbortSignal) {
  const digest = baseDepositDigest(q), idx = q.quoteIdx!;
  const original = await getBaseOrder(idx, signal);
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
  if (['awaiting_signature','expired'].includes(accepted.state)) throw new TypeError('Missing Base deposit authorization');
  return accepted;
}
