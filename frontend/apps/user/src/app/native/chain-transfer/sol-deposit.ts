import { decodeData } from '@dexwallet/api-client';
import type { BeneficiaryPaymentsData, DepositQuoteData, SolDepositOrderData } from '@dexwallet/api-contract/platform';
import { base58, base64 } from '@scure/base';
import { bytesToHex, concatBytes } from '@noble/hashes/utils.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { ed25519 } from '@noble/curves/ed25519.js';
import { signWithSeed } from './sign-with-seed';
import { HDKey } from 'micro-key-producer/slip10.js';
import { userRequest } from '../../../api/account';
import { DERIVATION, WalletError } from '../local-wallet/crypto';
import { atomicAmount } from './api';

import { depositTokenMint, solanaDepositSOL as sol } from './sol-deposit-asset';

const tokenProgram = base58.decode('TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA');
const ataProgram = base58.decode('ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL');
const encoder = new TextEncoder();
const orders = '/v1/user/sol-deposit-orders/';
const idxFormat = /^[a-f0-9]{32}$/;
const memoProgram = base58.decode('MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr');
export const lastSolQuoteKey = (userIdx: string, forOther = false) =>
  (forOther ? 'dexwallet:sol-deposit:last-beneficiary:' : 'dexwallet:sol-deposit:last:') + userIdx;

export function getBeneficiaryPayments(before: string, signal: AbortSignal) {
  return userRequest('/v1/user/sol-beneficiary-payments' + (before ? '?before=' + encodeURIComponent(before) : ''), value => {
    const page = decodeData<BeneficiaryPaymentsData>(value);
    if (!Array.isArray(page.items) || page.items.some(p => !p || !idxFormat.test(p.quoteIdx) ||
      !/^0\d{16,19}$/.test(p.beneficiaryIdx) || (atomicAmount(p.payQuantity, 9) ?? 0n) <= 0n ||
      (atomicAmount(p.targetUsd, 2) ?? 0n) <= 0n || !Number.isSafeInteger(p.acceptedAt) || p.acceptedAt <= 0) ||
      (page.nextCursor !== undefined && page.nextCursor !== page.items.at(-1)?.quoteIdx)) throw new TypeError('Invalid SOL payment history');
    return page;
  }, { signal });
}

function orderData(value: unknown, idx: string) {
  const o = decodeData<SolDepositOrderData>(value);
  if ((o.beneficiaryIdx !== undefined && (!/^0\d{16,19}$/.test(o.beneficiaryIdx) || o.assetId !== sol)) || o.quoteIdx !== idx || !idxFormat.test(o.orderIdx) || typeof o.messageBase64 !== 'string' || o.messageBase64.length > 1024 ||
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
  const assetId = o.assetId ?? sol; // older authorization-only servers returned native SOL
  const mint = depositTokenMint(assetId);
  if ((assetId !== sol && !mint) || (mint && final
    ? typeof o.actualFundingLamports !== 'string' || !/^(0|[1-9]\d{0,18})$/.test(o.actualFundingLamports) ||
      (executionState === 'failed' && o.actualFundingLamports !== '0')
    : o.actualFundingLamports !== undefined)) throw new TypeError('Invalid Solana account funding');
  if (o.creditedUsd !== undefined || o.creditedAt !== undefined) {
    if (executionState !== 'finalized' || typeof o.creditedUsd !== 'string' || !/^(0|[1-9]\d{0,17})\.\d{2}$/.test(o.creditedUsd) ||
      o.creditedUsd === '0.00' || !Number.isSafeInteger(o.creditedAt) || o.creditedAt! < o.acceptedAt!) throw new TypeError('Invalid SOL credit');
  }
  return { ...o, assetId, executionState };
}
export function getSolOrder(idx: string, signal: AbortSignal) {
  if (!idxFormat.test(idx)) throw new TypeError('Invalid quote ID');
  return userRequest(orders + idx, v => orderData(v, idx), { signal });
}

// Solana derives each associated token account from owner, token program and mint.
function associatedTokenAccount(owner: Uint8Array, mint: Uint8Array) {
  for (let bump = 255; bump >= 0; bump--) {
    const candidate = sha256(concatBytes(owner, tokenProgram, mint, Uint8Array.of(bump), ataProgram, encoder.encode('ProgramDerivedAddress')));
    try { ed25519.Point.fromBytes(candidate, true); } catch { return candidate; }
  }
  throw new TypeError('Invalid associated token account');
}

function tokenDepositMessage(msg: Uint8Array, mint: Uint8Array, payer: Uint8Array, sender: Uint8Array, recipient: Uint8Array, quantity: Uint8Array, memo: Uint8Array, funding: string | undefined) {
  const cap = funding === undefined ? undefined : atomicAmount(funding, 9);
  if (cap === undefined) throw new TypeError('Missing token account storage funding');
  const create = cap > 0n;
  const source = associatedTokenAccount(sender, mint), destination = associatedTokenAccount(recipient, mint);
  const keys = create
    ? [payer, sender, destination, source, ataProgram, recipient, mint, new Uint8Array(32), tokenProgram, memoProgram]
    : [payer, sender, source, destination, tokenProgram, mint, memoProgram];
  const offset = 4 + keys.length * 32;
  const instructions = create
    ? Uint8Array.of(3, 4, 6, 0, 2, 5, 6, 7, 8, 1, 1, 8, 4, 3, 6, 2, 1, 10, 12)
    : Uint8Array.of(2, 4, 4, 2, 5, 3, 1, 10, 12);
  return concatBytes(Uint8Array.of(2, 1, keys.length - 4, keys.length), ...keys, msg.slice(offset, offset + 32),
    instructions, quantity, Uint8Array.of(6, keys.length - 1, 0, memo.length), memo);
}

// Rebuild the signed transfer, optional ATA creation and order memo before unlocking the wallet.
export function checkedSolMessage(q: DepositQuoteData, o: SolDepositOrderData) {
  const mint = depositTokenMint(q.assetId);
  if (q.beneficiaryIdx !== o.beneficiaryIdx || !q.quoteIdx || q.quoteIdx !== o.quoteIdx || !q.solOrderIdx || !idxFormat.test(q.solOrderIdx) || q.solOrderIdx !== o.orderIdx ||
    q.expiresAt !== o.quoteExpiresAt || (q.assetId !== sol && !mint) || q.assetId !== (o.assetId ?? sol) || q.gasAssetId !== sol ||
    !q.walletAddress || !q.solFeePayer || !q.solRecipient || new Set([q.walletAddress,q.solFeePayer,q.solRecipient]).size !== 3 ||
    q.estimatedGas !== q.maxGas || q.messageSha256 !== o.messageSha256) throw new TypeError('SOL order differs from wallet quote');
  const msg = base64.decode(o.messageBase64);
  const amount = atomicAmount(q.payQuantity, mint ? 6 : 9);
  if (amount === undefined || amount <= 0n || amount > 0xffffffffffffffffn) throw new TypeError('Invalid SOL transfer amount');
  const quantity = new Uint8Array(8);
  new DataView(quantity.buffer).setBigUint64(0, amount, true);
  const suffix = q.beneficiaryIdx ? ':' + bytesToHex(sha256(encoder.encode(o.orderIdx + ':' + q.beneficiaryIdx))) : '';
  const memo = encoder.encode('dexwallet:deposit:' + o.orderIdx + suffix);
  const keys = [q.solFeePayer, q.walletAddress, q.solRecipient].map(s => base58.decode(s));
  if (keys.some(k => k.length !== 32)) throw new TypeError('Invalid SOL account');
  const header = Uint8Array.of(2, 0, 2, 5);
  const blockStart = header.length + 5 * 32;
  const expected = mint ? tokenDepositMessage(msg, mint, keys[0]!, keys[1]!, keys[2]!, quantity, memo, q.accountFunding) : concatBytes(header, ...keys, new Uint8Array(32), memoProgram, msg.slice(blockStart, blockStart + 32),
    Uint8Array.of(2, 3, 2, 1, 2, 12, 2, 0, 0, 0), quantity, Uint8Array.of(4, 0, memo.length), memo);
  if (expected.length !== msg.length || !expected.every((b, i) => b === msg[i]) || bytesToHex(sha256(msg)) !== q.messageSha256) throw new TypeError('Unexpected SOL signing message');
  return msg;
}

export async function signLocalSolDeposit(userIdx: string, walletId: string, q: DepositQuoteData, o: SolDepositOrderData, signal: AbortSignal) {
  const msg = checkedSolMessage(q, o);
  return signWithSeed(userIdx, walletId, signal, seed => {
    const root = HDKey.fromMasterSeed(seed), key = root.derive(DERIVATION.solana);
    try {
      if (base58.encode(key.publicKeyRaw) !== q.walletAddress) throw new WalletError('LOCAL_VIEW_CLOSED');
      return base58.encode(ed25519.sign(msg, key.privateKey));
    } finally {
      for (const node of [root, key]) { node.privateKey.fill(0); node.chainCode.fill(0); }
    }
  });
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
  if (accepted.beneficiaryIdx !== quote.beneficiaryIdx || accepted.state !== 'accepted' || accepted.assetId !== quote.assetId || accepted.orderIdx !== order.orderIdx || accepted.messageSha256 !== order.messageSha256) throw new TypeError('Invalid SOL acceptance');
  return accepted;
}
