import { decodeData } from '@dexwallet/api-client';
import type { DepositQuoteData, TonDepositOrderData } from '@dexwallet/api-contract/platform';
import { Address, beginCell, Cell, contractAddress, internal, SendMode, WalletContractV5R1 } from '@ton/ton';
import { storeOutListExtendedV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1Actions.js';
import { storeWalletIdV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1WalletId.js';
import { mnemonicToPrivateKey, keyPairFromSeed, sign } from '@ton/crypto';
import { sha256 } from '@noble/hashes/sha2.js';
import { mnemonicToSeed } from '@scure/bip39';
import { HDKey } from 'micro-key-producer/slip10.js';
import { Buffer } from '../local-wallet/buffer';
import { DERIVATION, WalletError, type WalletMaterial } from '../local-wallet/crypto';
import { userRequest } from '../../../api/account';
import { atomicAmount } from './api';
import { testJettonAsset, testJettonMaster } from './ton-deposit-asset';
import { signWithMaterial } from './sign-with-seed';

const path = '/v1/user/ton-deposit-orders/';
const idxPattern = /^[a-f0-9]{32}$/;
const hashPattern = /^[a-f0-9]{64}$/;
export const lastTonQuoteKey = (user: string) => 'dexwallet:ton-deposit:last:' + user;
const network = { networkGlobalId: -3, context: { walletVersion: 'v5r1' as const, workchain: 0, subwalletNumber: 0 } };

export function tonDepositDigests(q: DepositQuoteData) {
  const token = q.assetId === testJettonAsset;
  const atoms = atomicAmount(q.payQuantity, token ? 6 : 9);
  if (!q.quoteIdx || !idxPattern.test(q.quoteIdx) || (!token && q.assetId !== 'ton-testnet:ton') || q.gasAssetId !== 'ton-testnet:ton' ||
      !q.walletFeePreview || !q.walletAddress || !q.tonRecipient || !/^0:[a-f0-9]{64}$/.test(q.tonRecipient) ||
      !q.tonAuthorizationHash || !hashPattern.test(q.tonAuthorizationHash) || !atoms || atoms >= (1n << 120n) ||
      q.relayAttached !== (token ? '0.053' : '0.003') || !q.maxGas || q.maxGas !== q.estimatedGas || !atomicAmount(q.maxGas, 9) ||
      !Number.isSafeInteger(q.tonValidUntil) || q.tonValidUntil! <= q.expiresAt / 1000 || q.tonValidUntil! > 0xffffffff ||
      !Number.isSafeInteger(q.tonUserSeqno) || q.tonUserSeqno! <= 0 || q.tonUserSeqno! > 0xffffffff) throw new TypeError('Invalid TON deposit terms');
  const sender = Address.parse(q.walletAddress), recipient = Address.parseRaw(q.tonRecipient);
  if (sender.workChain !== 0 || sender.equals(recipient)) throw new TypeError('Invalid TON deposit addresses');
  let msg;
  if (token) {
    if (!q.tonJettonWalletCode || !/^(?:[a-f0-9]{2}){1,4096}$/.test(q.tonJettonWalletCode)) throw new TypeError('Invalid Jetton wallet code');
    const [code, ...others] = Cell.fromBoc(Buffer.from(q.tonJettonWalletCode, 'hex'));
    if (!code || others.length || code.hash().toString('base64') !== 'j0Utek39dAZraCNlF3JZ7QVzRDW+drX9S9XYryt8PWg=') throw new TypeError('Unsupported test Jetton wallet code');
    const data = beginCell().storeUint(0, 4).storeCoins(0).storeAddress(sender).storeAddress(Address.parseRaw(testJettonMaster)).endCell();
    const to = contractAddress(0, { code, data });
    const body = beginCell().storeUint(0x0f8a7ea5, 32).storeUint(q.tonValidUntil!, 64).storeCoins(atoms)
      .storeAddress(recipient).storeAddress(recipient).storeBit(false).storeCoins(1n).storeBit(false).endCell();
    msg = internal({ to, value: 50_000_000n, bounce: true, body });
  } else {
    if (q.tonJettonWalletCode !== undefined) throw new TypeError('Unexpected Jetton code');
    msg = internal({ to: recipient, value: atoms, bounce: false });
  }
  const payload = beginCell().storeUint(WalletContractV5R1.OpCodes.auth_signed_internal, 32)
    .store(storeWalletIdV5R1(network)).storeUint(q.tonValidUntil!, 32).storeUint(q.tonUserSeqno!, 32)
    .store(storeOutListExtendedV5R1([{ type: 'sendMsg', mode: SendMode.PAY_GAS_SEPARATELY | SendMode.IGNORE_ERRORS, outMsg: msg }])).endCell();
  if (payload.hash().toString('hex') !== q.tonAuthorizationHash) throw new TypeError('TON payment differs from confirmed quote');
  const domain = token ? 'DexWallet/1/' + testJettonAsset + '/self-deposit/' : 'DexWallet/1/ton-testnet/ton/self-deposit/';
  const order = sha256(new TextEncoder().encode(domain + q.quoteIdx + '/' + q.tonAuthorizationHash));
  return { user: payload.hash(), order: Buffer.from(order), sender };
}

async function tonKey(material: WalletMaterial) {
  if (material.tonVersion !== 'v5r1') throw new WalletError('LOCAL_INVALID_SECRET');
  if (material.scheme === 'ton') return mnemonicToPrivateKey(material.words.split(' '));
  const seed = await mnemonicToSeed(material.words, '');
  const root = HDKey.fromMasterSeed(seed), child = root.derive(DERIVATION.ton);
  const raw = Buffer.from(child.privateKey);
  try { return keyPairFromSeed(raw); }
  finally {
    raw.fill(0); seed.fill(0);
    for (const k of [root, child]) { k.privateKey.fill(0); k.chainCode.fill(0); }
  }
}
function orderData(value: unknown, idx: string) {
  const o = decodeData<TonDepositOrderData>(value);
  const costs = [o.jettonSenderFeeNano, o.recoverableFeeNano, o.jettonExcessNano, o.jettonReserveNano, o.relaySurplusNano];
  if (costs.some(v => v !== undefined) && (!o.receiveTxHash || !o.actualFeeNano || o.jettonExcessNano === '0' ||
      costs.some(v => typeof v !== 'string' || !/^(0|[1-9]\d*)$/.test(v)))) throw new TypeError('Invalid Jetton accounting');
  if (o.recoverableFeeNano !== undefined && (!o.actualFeeNano || !/^[1-9]\d*$/.test(o.actualFeeNano) ||
      BigInt(o.recoverableFeeNano) + BigInt(o.jettonSenderFeeNano!) !== BigInt(o.actualFeeNano))) throw new TypeError('Inconsistent Jetton network fee');
  if (o.quoteIdx !== idx || !['awaiting_signature','expired','accepted','signed','credited','review'].includes(o.state) ||
      [o.dispatchBodyHash, o.receiveTxHash].some(h => h !== undefined && !hashPattern.test(h)) ||
      (o.actualFeeNano !== undefined && !/^[1-9]\d*$/.test(o.actualFeeNano)) ||
      (['signed','credited'].includes(o.state) && !o.dispatchBodyHash) ||
      (o.state === 'credited' ? !o.receiveTxHash || !o.actualFeeNano || !o.creditedUsd || !/^\d+\.\d{2}$/.test(o.creditedUsd) || o.creditedUsd === '0.00' : o.creditedUsd !== undefined))
    throw new TypeError('Invalid TON deposit order');
  return o;
}
export function getTonOrder(idx: string, signal: AbortSignal) {
  if (!idxPattern.test(idx)) throw new TypeError('Invalid TON quote ID');
  return userRequest(path + idx, v => orderData(v, idx), { signal });
}
export async function authorizeTonDeposit(user: string, walletId: string, q: DepositQuoteData, deadline: number, signal: AbortSignal) {
  const d = tonDepositDigests(q), idx = q.quoteIdx!;
  const original = await getTonOrder(idx, signal);
  if (original.state !== 'awaiting_signature') return original;
  const check = () => {
    signal.throwIfAborted();
    if (document.visibilityState === 'hidden' || Date.now() >= deadline || Date.now() >= q.tonValidUntil! * 1000) throw new WalletError('LOCAL_VIEW_CLOSED');
  };
  check();
  const body = await signWithMaterial(user, walletId, signal, async m => {
    const key = await tonKey(m);
    try {
      check();
      if (!WalletContractV5R1.create({ publicKey: key.publicKey, walletId: network }).address.equals(d.sender)) throw new WalletError('LOCAL_VIEW_CLOSED');
      return { quoteIdx: idx, userSignature: sign(d.user, key.secretKey).toString('hex'), orderSignature: sign(d.order, key.secretKey).toString('hex') };
    } finally { key.secretKey.fill(0); }
  });
  check();
  const o = await userRequest(path + 'authorize', v => orderData(v, idx), { method: 'POST', body, signal });
  if (o.state === 'awaiting_signature' || o.state === 'expired') throw new TypeError('Missing TON authorization');
  return o;
}
