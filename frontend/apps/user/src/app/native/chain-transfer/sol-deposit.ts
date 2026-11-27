import { decodeData } from '@dexwallet/api-client';
import type { DepositQuoteData, SolDepositOrderData } from '@dexwallet/api-contract/platform';
import { base58, base64 } from '@scure/base';
import { bytesToHex, concatBytes } from '@noble/hashes/utils.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { ed25519 } from '@noble/curves/ed25519.js';
import { mnemonicToSeed } from '@scure/bip39';
import { HDKey } from 'micro-key-producer/slip10.js';
import { userRequest } from '../../../api/account';
import { nativeVault } from '../local-wallet/bridge';
import { DERIVATION, parseMaterial, WalletError } from '../local-wallet/crypto';
import { atomicAmount } from './api';

const orders = '/v1/user/sol-deposit-orders/';
const idxFormat = /^[a-f0-9]{32}$/;
const memoProgram = base58.decode('MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr');
export const lastSolQuoteKey = (userIdx: string) => 'dexwallet:sol-deposit:last:' + userIdx;

function orderData(value: unknown, idx: string) {
  const o = decodeData<SolDepositOrderData>(value);
  if (o.quoteIdx !== idx || !idxFormat.test(o.orderIdx) || typeof o.messageBase64 !== 'string' || o.messageBase64.length > 512 ||
    !/^[a-f0-9]{64}$/.test(o.messageSha256) || !Number.isSafeInteger(o.lastValidBlockHeight) || o.lastValidBlockHeight <= 0 ||
    !Number.isSafeInteger(o.quoteExpiresAt) || !Number.isSafeInteger(o.reviewAfter) || o.reviewAfter <= o.quoteExpiresAt ||
    !(o.state === 'awaiting_signature' && o.acceptedAt === undefined ||
      o.state === 'accepted' && Number.isSafeInteger(o.acceptedAt) && o.acceptedAt! > 0 && o.acceptedAt! < o.quoteExpiresAt)) throw new TypeError('Invalid SOL deposit order');
  const executionState = o.executionState ?? 'waiting'; // servers serving authorization-only orders
  const final = executionState === 'finalized' || executionState === 'failed';
  if (!['waiting','signed','submitted','review','finalized','failed'].includes(executionState) ||
    (o.state !== 'accepted' && executionState !== 'waiting') ||
    (executionState === 'waiting' && o.txSignature !== undefined) ||
    (executionState !== 'waiting' && executionState !== 'review' && !o.txSignature) ||
    (o.txSignature !== undefined && !/^[1-9A-HJ-NP-Za-km-z]{64,88}$/.test(o.txSignature)) ||
    (final ? !Number.isSafeInteger(o.actualFeeLamports) || o.actualFeeLamports! <= 0 : o.actualFeeLamports !== undefined)) {
    throw new TypeError('Invalid SOL execution result');
  }
  return { ...o, executionState };
}
export function getSolOrder(idx: string, signal: AbortSignal) {
  if (!idxFormat.test(idx)) throw new TypeError('Invalid quote ID');
  return userRequest(orders + idx, v => orderData(v, idx), { signal });
}

// Rebuild the only supported instruction sequence before unlocking the selected wallet.
export function checkedSolMessage(q: DepositQuoteData, o: SolDepositOrderData) {
  if (!q.quoteIdx || q.quoteIdx !== o.quoteIdx || !q.solOrderIdx || !idxFormat.test(q.solOrderIdx) || q.solOrderIdx !== o.orderIdx ||
    q.expiresAt !== o.quoteExpiresAt || q.assetId !== 'solana-devnet:sol' || q.gasAssetId !== q.assetId ||
    !q.walletAddress || !q.solFeePayer || !q.solRecipient || new Set([q.walletAddress,q.solFeePayer,q.solRecipient]).size !== 3 ||
    q.estimatedGas !== q.maxGas || q.messageSha256 !== o.messageSha256) throw new TypeError('SOL order differs from wallet quote');
  const msg = base64.decode(o.messageBase64);
  const amount = atomicAmount(q.payQuantity, 9);
  if (amount === undefined || amount <= 0n || amount > 0xffffffffffffffffn) throw new TypeError('Invalid SOL transfer amount');
  const lamports = new Uint8Array(8);
  new DataView(lamports.buffer).setBigUint64(0, amount, true);
  const memo = new TextEncoder().encode('dexwallet:deposit:' + o.orderIdx);
  const keys = [q.solFeePayer, q.walletAddress, q.solRecipient].map(s => base58.decode(s));
  if (keys.some(k => k.length !== 32)) throw new TypeError('Invalid SOL account');
  const header = Uint8Array.of(2, 0, 2, 5);
  const blockStart = header.length + 5 * 32;
  const expected = concatBytes(header, ...keys, new Uint8Array(32), memoProgram, msg.slice(blockStart, blockStart + 32),
    Uint8Array.of(2, 3, 2, 1, 2, 12, 2, 0, 0, 0), lamports, Uint8Array.of(4, 0, memo.length), memo);
  if (expected.length !== msg.length || !expected.every((b, i) => b === msg[i]) || bytesToHex(sha256(msg)) !== q.messageSha256) throw new TypeError('Unexpected SOL signing message');
  return msg;
}

export async function signLocalSolDeposit(userIdx: string, walletId: string, q: DepositQuoteData, o: SolDepositOrderData, signal: AbortSignal) {
  const msg = checkedSolMessage(q, o);
  const token = crypto.randomUUID();
  const close = () => { void nativeVault.unprotect({ token }).catch(() => {}); };
  signal.throwIfAborted();
  const listener = await nativeVault.addListener('privacyChange', state => {
    if (state.token === token && state.active && !signal.aborted) void nativeVault.acknowledge({ token, revision: state.revision }).catch(() => {});
  });
  try {
    signal.throwIfAborted();
    signal.addEventListener('abort', close, { once: true });
    const state = await nativeVault.protect({ token });
    if (!state.active) throw new WalletError('LOCAL_VIEW_CLOSED');
    await nativeVault.acknowledge({ token, revision: state.revision });
    signal.throwIfAborted();
    const { secret } = await nativeVault.get({ userIdx, walletId, viewToken: token });
    const material = await parseMaterial(secret);
    if (material.scheme !== 'bip39') throw new WalletError('LOCAL_INVALID_SECRET');
    const seed = await mnemonicToSeed(material.words, '');
    const root = HDKey.fromMasterSeed(seed), key = root.derive(DERIVATION.solana);
    try {
      signal.throwIfAborted();
      if (document.visibilityState === 'hidden' || base58.encode(key.publicKeyRaw) !== q.walletAddress) throw new WalletError('LOCAL_VIEW_CLOSED');
      return base58.encode(ed25519.sign(msg, key.privateKey));
    } finally {
      seed.fill(0);
      for (const node of [root, key]) { node.privateKey.fill(0); node.chainCode.fill(0); }
    }
  } finally {
    signal.removeEventListener('abort', close);
    try { await nativeVault.unprotect({ token }); } finally { await listener.remove(); }
  }
}

export async function authorizeSolDeposit(userIdx: string, walletId: string, quote: DepositQuoteData, deadline: number, signal: AbortSignal) {
  const idx = quote.quoteIdx!;
  const read = (value: unknown) => orderData(value, idx);
  const order = await userRequest(orders + 'prepare', read, { method: 'POST', body: { quoteIdx: idx }, signal });
  checkedSolMessage(quote, order);
  if (order.state === 'accepted') return order;
  const checkTime = () => { signal.throwIfAborted(); if (Date.now() >= deadline) throw new Error('Quote expired'); };
  checkTime();
  const userSignature = await signLocalSolDeposit(userIdx, walletId, quote, order, signal);
  checkTime();
  const accepted = await userRequest(orders + 'authorize', read, { method: 'POST', body: { quoteIdx: idx, userSignature }, signal });
  if (accepted.state !== 'accepted' || accepted.orderIdx !== order.orderIdx || accepted.messageSha256 !== order.messageSha256) throw new TypeError('Invalid SOL acceptance');
  return accepted;
}
