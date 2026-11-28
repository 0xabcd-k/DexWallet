import { decodeData } from '@dexwallet/api-client';
import type { DepositQuoteData, TronDepositOrderData } from '@dexwallet/api-contract/platform';
import { secp256k1 } from '@noble/curves/secp256k1.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { keccak_256 } from '@noble/hashes/sha3.js';
import { bytesToHex, concatBytes, hexToBytes } from '@noble/hashes/utils.js';
import { HDKey } from '@scure/bip32';
import { base58check } from '@scure/base';
import { userRequest } from '../../../api/account';
import { DERIVATION, WalletError } from '../local-wallet/crypto';
import { atomicAmount } from './api';
import { nileDepositTRX, nileDepositUSDT, nileDepositUSDTContract, nileDepositEnergyLimitSun } from './tron-deposit-asset';
import { signWithSeed } from './sign-with-seed';

const orders = '/v1/user/tron-deposit-orders/';
const id = /^[a-f0-9]{32}$/;
const hash = /^[a-f0-9]{64}$/;
const text = new TextEncoder();
export const nileTransferLifetime = 600_000;
export const lastTronQuoteKey = (userIdx: string) => 'dexwallet:tron-deposit:last:' + userIdx;
function address(value: string | undefined) {
  if (!value || value.length !== 34) throw new TypeError('Invalid Nile address');
  const raw = base58check(sha256).decode(value);
  if (raw.length !== 21 || raw[0] !== 0x41 || !raw.slice(1).some(b => b !== 0)) throw new TypeError('Invalid Nile address');
  return raw;
}
function varint(value: bigint) {
  const out: number[] = [];
  while (value >= 128n) { out.push(Number(value & 127n) | 128); value >>= 7n; }
  return Uint8Array.from([...out, Number(value)]);
}
function bytesField(field: number, data: Uint8Array) {
  return concatBytes(Uint8Array.of(field * 8 + 2), varint(BigInt(data.length)), data);
}
function numberField(field: number, value: bigint) {
  return concatBytes(varint(BigInt(field * 8)), varint(value));
}

// Reconstruct the only Nile contract call that this quote authorizes.
export function nileDepositDigests(q: DepositQuoteData) {
  const token = q.assetId === nileDepositUSDT;
  const amount = atomicAmount(q.payQuantity, 6);
  if (!q.quoteIdx || !id.test(q.quoteIdx) || !q.messageSha256 || !hash.test(q.messageSha256) ||
      (!token && q.assetId !== nileDepositTRX) || q.gasAssetId !== nileDepositTRX || !q.walletFeePreview ||
      q.estimatedGas !== '0' || q.maxGas !== '0' || (token ? !Number.isSafeInteger(q.delegatedEnergy) || !(q.delegatedEnergy! > 0) : q.delegatedEnergy !== undefined) ||
      !amount || amount > (token ? (1n << 256n) - 1n : 0x7fff_ffff_ffff_ffffn) || q.walletAddress === q.tronRecipient || q.walletAddress === q.tronResourceOwner ||
      !q.tronRefBlock || !hash.test(q.tronRefBlock) || !q.tronRefTime || !Number.isSafeInteger(q.tronRefTime + nileTransferLifetime) ||
      q.tronRefTime <= 0 || q.tronRefTime + nileTransferLifetime <= q.expiresAt) throw new TypeError('Invalid Nile signing terms');
  const sender = address(q.walletAddress), recipient = address(q.tronRecipient);
  address(q.tronResourceOwner);
  const head = hexToBytes(q.tronRefBlock);
  if (!head.slice(0, 8).some(b => b !== 0)) throw new TypeError('Invalid Nile reference height');
  const method = token ? 'TriggerSmartContract' : 'TransferContract';
  const value = token ? concatBytes(bytesField(1, sender), bytesField(2, address(nileDepositUSDTContract)),
    bytesField(4, concatBytes(hexToBytes('a9059cbb'), new Uint8Array(12), recipient.slice(1), hexToBytes(amount.toString(16).padStart(64, '0')))))
    : concatBytes(bytesField(1, sender), bytesField(2, recipient), numberField(3, amount));
  const any = concatBytes(bytesField(1, text.encode('type.googleapis.com/protocol.' + method)), bytesField(2, value));
  const contract = concatBytes(numberField(1, token ? 31n : 1n), bytesField(2, any));
  const raw = concatBytes(bytesField(1, head.slice(6, 8)), bytesField(4, head.slice(8, 16)),
    numberField(8, BigInt(q.tronRefTime + nileTransferLifetime)), bytesField(11, contract), numberField(14, BigInt(q.tronRefTime)),
    token ? numberField(18, BigInt(nileDepositEnergyLimitSun)) : new Uint8Array());
  const tx = sha256(raw);
  const points = bytesField(1, raw).length + bytesField(2, new Uint8Array(65)).length + 64;
  if (bytesToHex(tx) !== q.messageSha256 || points !== q.delegatedBandwidth) throw new TypeError('Nile transfer differs from quote');
  return { tx, order: sha256(text.encode('DexWallet/1/' + (token ? nileDepositUSDT : 'tron-nile/trx') + '/self-deposit/' + q.quoteIdx + '/' + q.messageSha256)) };
}

function orderData(value: unknown, idx: string) {
  const raw = decodeData<TronDepositOrderData>(value);
  // TRX orders served before business 46 had no asset field.
  const o = raw.assetId === undefined ? { ...raw, assetId: nileDepositTRX } : raw;
  const waiting = o.state === 'awaiting_signature' || o.state === 'expired';
  if ((o.assetId !== nileDepositUSDT && o.assetId !== nileDepositTRX) || o.quoteIdx !== idx || !['awaiting_signature','expired','signed','review','credited'].includes(o.state) ||
      (waiting ? o.txId !== undefined : typeof o.txId !== 'string' || !hash.test(o.txId)) ||
      (o.actualFeeSun !== undefined && (typeof o.actualFeeSun !== 'string' || !/^(0|[1-9]\d{0,18})$/.test(o.actualFeeSun))) ||
      (o.actualBandwidth !== undefined && (!Number.isSafeInteger(o.actualBandwidth) || o.actualBandwidth <= 0)) ||
      (o.actualEnergy !== undefined && (!Number.isSafeInteger(o.actualEnergy) || o.actualEnergy < 0)) ||
      (o.state === 'credited' ? (o.assetId === nileDepositUSDT ? !o.actualEnergy : !!o.actualEnergy) || o.actualFeeSun !== '0' || !o.actualBandwidth || !o.creditedUsd || !/^\d+\.\d{2}$/.test(o.creditedUsd) || o.creditedUsd === '0.00' : o.creditedUsd !== undefined)) {
    throw new TypeError('Invalid Nile deposit status');
  }
  return o;
}
export function getTronOrder(idx: string, signal: AbortSignal) {
  if (!id.test(idx)) throw new TypeError('Invalid Nile order ID');
  return userRequest(orders + idx, v => orderData(v, idx), { signal });
}
export async function authorizeTronDeposit(userIdx: string, walletId: string, q: DepositQuoteData, deadline: number, signal: AbortSignal) {
  const { tx, order } = nileDepositDigests(q), idx = q.quoteIdx!;
  const original = await getTronOrder(idx, signal);
  if (original.assetId !== q.assetId || (original.txId && original.txId !== q.messageSha256)) throw new TypeError('Unexpected Nile order transaction');
  if (original.state !== 'awaiting_signature') return original;
  const check = () => { signal.throwIfAborted(); if (Date.now() >= deadline) throw new Error('Quote expired'); };
  check();
  const body = await signWithSeed(userIdx, walletId, signal, seed => {
    const root = HDKey.fromMasterSeed(seed), key = root.derive(DERIVATION.tron);
    try {
      check();
      if (!key.privateKey) throw new WalletError('LOCAL_DERIVATION_FAILED');
      const pub = secp256k1.getPublicKey(key.privateKey, false);
      if (bytesToHex(keccak_256(pub.slice(1)).slice(-20)) !== bytesToHex(address(q.walletAddress).slice(1))) throw new WalletError('LOCAL_VIEW_CLOSED');
      const sign = (digest: Uint8Array) => {
        const sig = secp256k1.sign(digest, key.privateKey!, { prehash: false, format: 'recovered' });
        return bytesToHex(concatBytes(sig.slice(1), sig.slice(0, 1)));
      };
      return { quoteIdx: idx, userSignature: sign(tx), orderSignature: sign(order) };
    } finally { key.wipePrivateData(); root.wipePrivateData(); }
  });
  check();
  const accepted = await userRequest(orders + 'authorize', v => orderData(v, idx), { method: 'POST', body, signal });
  if (accepted.assetId !== q.assetId || accepted.txId !== q.messageSha256) throw new TypeError('Missing Nile deposit authorization');
  return accepted;
}
