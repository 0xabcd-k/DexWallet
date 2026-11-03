import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { Address, Cell, TonClient, WalletContractV5R1, beginCell, internal, external, storeMessage, loadMessageRelaxed, contractAddress, loadStateInit } from '@ton/ton';
import { mnemonicToPrivateKey, signVerify } from '@ton/crypto';
import { loadOutListExtendedV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1Actions.js';
import { storeWalletIdV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1WalletId.js';

// Dedicated TON testnet experiment. Fixed accounts/amounts; no mainnet endpoint.
export const USER = '0:a393a594688a53f6138d88f3447a8991093cc387f0463d0acd955289fadbef7a';
export const PUBLIC_KEY = '91a0d3523098850dc441f525f5fb22c8e25a7470a3888aada6868b093b37cb51';
export const PLATFORM = '0:100769e6dae579cfbc5ca1637c88d6c83a6c2195b3e571f1e35f20b45b2fa4d0';
export const PAYMENT = 10_000_000n; // 0.01 TON principal.
export const RELAY_VALUE = 3_000_000n; // 0.003 TON upper budget; surplus is measured, not called a fee.
const FUNDING = 50_000_000n;
export const directory = new URL('../../.tools/payment-feasibility/ton-live/', import.meta.url);
const userWallet = WalletContractV5R1.create({ publicKey: Buffer.from(PUBLIC_KEY, 'hex'), walletId: { networkGlobalId: -3 } });
const walletId = beginCell().store(storeWalletIdV5R1(userWallet.walletId)).endCell().beginParse().loadUint(32);
const client = new TonClient({ endpoint: 'https://testnet.toncenter.com/api/v2/jsonRPC', timeout: 20000 });
const json = value => JSON.stringify(value, (_key, v) => typeof v === 'bigint' ? v.toString() : v, 2) + '\n';
let lastRpc = 0;
async function rpc(fn) {
  await new Promise(resolve => setTimeout(resolve, Math.max(0, 3000 - (Date.now() - lastRpc))));
  try { return await fn(); } finally { lastRpc = Date.now(); }
}
export async function snapshot() {
  const result = { observedAt: new Date().toISOString() };
  for (const [role, raw] of [['platform', PLATFORM], ['user', USER]]) {
    const s = await rpc(() => client.getContractState(Address.parse(raw)));
    let seqno = 0;
    if (s.state === 'active') {
      const data = Cell.fromBoc(s.data)[0].beginParse();
      assert(data.loadBoolean(), `${role} public-key signing disabled`);
      seqno = data.loadUint(32);
      if (role === 'user') {
        assert(Cell.fromBoc(s.code)[0].equals(userWallet.init.code), 'Unexpected user wallet code');
        assert.equal(data.loadUint(32), walletId);
        assert.equal(data.loadBuffer(32).toString('hex'), PUBLIC_KEY);
        assert.equal(data.loadBoolean(), false, 'Unexpected wallet extensions');
      }
    }
    result[role] = { address: raw, state: s.state, balance: s.balance.toString(), seqno, lastTransaction: s.lastTransaction };
  }
  return result;
}

export function validatePayment(capture, expectedSeqno, now = Math.floor(Date.now() / 1000)) {
  assert.equal(capture.account.chain, '-3');
  assert.equal(capture.request.network, '-3');
  assert.equal(capture.account.address, USER);
  assert.equal(capture.account.publicKey, PUBLIC_KEY);
  assert.equal(capture.request.messages.length, 1);
  assert.equal(capture.request.messages[0].amount, PAYMENT.toString());
  assert(Address.parse(capture.request.messages[0].address).equals(Address.parse(PLATFORM)));
  const init = loadStateInit(Cell.fromBase64(capture.account.walletStateInit).beginParse());
  assert(contractAddress(0, init).equals(userWallet.address));
  const wrapper = loadMessageRelaxed(Cell.fromBase64(capture.result.internalBoc).beginParse());
  assert.equal(wrapper.info.type, 'internal');
  assert(wrapper.info.dest.equals(userWallet.address), 'Wrong outer relay destination');
  assert.equal(wrapper.info.value.coins, 0n);
  assert.equal(wrapper.info.src, null);
  assert.equal(wrapper.info.bounced, false);
  assert.equal(wrapper.init, null, 'Initialize wallet separately before payment');
  const s = wrapper.body.beginParse();
  const b = beginCell().storeBits(s.loadBits(s.remainingBits - 512));
  const sig = s.loadBuffer(64);
  while (s.remainingRefs) b.storeRef(s.loadRef());
  const unsigned = b.endCell();
  assert(signVerify(unsigned.hash(), sig, Buffer.from(PUBLIC_KEY, 'hex')), 'Invalid signature');
  const fields = unsigned.beginParse();
  assert.equal(fields.loadUint(32), WalletContractV5R1.OpCodes.auth_signed_internal);
  assert.equal(fields.loadUint(32), walletId, 'Wrong network walletId');
  const validUntil = fields.loadUint(32), seqno = fields.loadUint(32);
  assert.equal(seqno, expectedSeqno, 'Stale wallet seqno');
  assert(seqno > 0, 'First-use authorization must not enter ordinary payment');
  assert.equal(validUntil, capture.request.validUntil, 'Signed deadline mismatch');
  assert(validUntil > now + 30 && validUntil <= now + 300, 'Expired or excessive authorization lifetime');
  const actions = loadOutListExtendedV5R1(fields);
  assert.equal(fields.remainingBits + fields.remainingRefs, 0);
  assert.equal(actions.length, 1, 'Unexpected extra actions');
  const a = actions[0];
  assert.equal(a.type, 'sendMsg');
  assert.equal(a.mode, 3);
  assert.equal(a.outMsg.info.type, 'internal');
  assert(a.outMsg.info.dest.equals(Address.parse(PLATFORM)), 'Wrong payment destination');
  assert.equal(a.outMsg.info.value.coins, PAYMENT, 'Wrong payment amount');
  assert(!a.outMsg.info.value.other || a.outMsg.info.value.other.size === 0, 'Unexpected extra currencies');
  assert.equal(a.outMsg.init, null);
  assert.equal(a.outMsg.body.bits.length + a.outMsg.body.refs.length, 0, 'Unexpected payload');
  return { body: wrapper.body, validUntil, seqno, bodyHash: wrapper.body.hash().toString('hex') };
}

async function platformWallet() {
  // Only platform mnemonic is read. The imported user's key stays in the wallet.
  const phrase = (await readFile(new URL('../ton-acton/platform-phrase.txt', directory), 'utf8')).trim().split(/\s+/);
  const key = await mnemonicToPrivateKey(phrase);
  const wallet = WalletContractV5R1.create({ publicKey: key.publicKey, walletId: { networkGlobalId: -3 } });
  assert.equal(wallet.address.toRawString(), PLATFORM);
  return { key, wallet };
}
async function publish(name, message, before, authorization) {
  const statePath = new URL(`${name}.json`, directory);
  const { key, wallet } = await platformWallet();
  const timeout = Math.min(Math.floor(Date.now() / 1000) + 120, authorization?.validUntil ?? Infinity);
  const body = wallet.createTransfer({ seqno: before.platform.seqno, secretKey: key.secretKey, timeout, sendMode: 3, messages: [message] });
  const ext = beginCell().store(storeMessage(external({ to: wallet.address, init: before.platform.state === 'active' ? null : wallet.init, body }))).endCell();
  const record = { observedAt: new Date().toISOString(), name, before, authorization,
    platformBodyHash: body.hash().toString('hex'), externalHash: ext.hash().toString('hex'), status: 'prepared' };
  // Existing record prevents a second transaction after an uncertain submission.
  await writeFile(statePath, json(record), { mode: 0o600, flag: 'wx' });
  await writeFile(new URL(`${name}-wire.boc`, directory), ext.toBoc(), { mode: 0o600, flag: 'wx' });
  await rpc(() => {
    assert(timeout > Date.now() / 1000 + 15, 'Authorization too close to expiry; inspect prepared record');
    return client.sendFile(ext.toBoc());
  });
  record.status = 'broadcast-accepted';
  await writeFile(statePath, json(record), { mode: 0o600 });
  console.log(json({ name, status: record.status, externalHash: record.externalHash }));
  for (let i = 0; i < 30; i++) {
    await new Promise(resolve => setTimeout(resolve, 3000));
    const after = await snapshot();
    if (after.platform.seqno > before.platform.seqno && (name !== 'relay' || after.user.seqno > before.user.seqno)) {
      record.after = after; record.status = 'seqno-advanced-needs-trace';
      await writeFile(statePath, json(record), { mode: 0o600 });
      console.log(json(record)); return;
    }
  }
  throw new Error('Confirmation timeout; inspect saved state before retrying');
}
function messageSummary(m) {
  if (!m) return null;
  return { type: m.info.type, source: m.info.src?.toString(), destination: m.info.dest?.toString(),
    value: m.info.type === 'internal' ? m.info.value.coins : null,
    forwardFee: m.info.type === 'internal' ? m.info.forwardFee : null,
    bounced: m.info.type === 'internal' ? m.info.bounced : null, bodyHash: m.body.hash().toString('hex') };
}
async function report() {
  const result = { observedAt: new Date().toISOString(), snapshot: await snapshot(), transactions: {} };
  for (const [role, raw] of [['platform', PLATFORM], ['user', USER]]) {
    const txs = await rpc(() => client.getTransactions(Address.parse(raw), { limit: 10 }));
    result.transactions[role] = txs.map(t => ({ hash: t.hash().toString('hex'), lt: t.lt, now: t.now,
      oldStatus: t.oldStatus, endStatus: t.endStatus, totalFees: t.totalFees.coins,
      description: t.description, incoming: messageSummary(t.inMessage), outgoing: [...t.outMessages.values()].map(messageSummary) }));
  }
  await writeFile(new URL(`chain-report-${Date.now()}.json`, directory), json(result), { mode: 0o600 });
  console.log(json(result));
}
async function main() {
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const command = process.argv[2];
  if (command === 'status') { console.log(json(await snapshot())); return; }
  if (command === 'report') { await report(); return; }
  const before = await snapshot();
  if (command === 'fund') {
    assert.equal(before.user.balance, '0'); assert.equal(before.user.seqno, 0);
    assert(BigInt(before.platform.balance) > FUNDING + 10_000_000n);
    await publish('fund', internal({ to: USER, value: FUNDING, bounce: false }), before);
  } else if (command === 'relay') {
    assert.equal(before.user.state, 'active');
    assert(BigInt(before.user.balance) >= PAYMENT, 'User lacks principal');
    const capture = JSON.parse(await readFile(process.argv[3], 'utf8'));
    const auth = validatePayment(capture, before.user.seqno);
    assert(BigInt(before.platform.balance) > RELAY_VALUE + 10_000_000n);
    await publish('relay', internal({ to: USER, value: RELAY_VALUE, bounce: true, body: auth.body }), before,
      { validUntil: auth.validUntil, seqno: auth.seqno, bodyHash: auth.bodyHash, amount: PAYMENT, relayValue: RELAY_VALUE });
  } else throw new Error('Usage: ton-live.mjs status|report|fund|relay PRIVATE_CAPTURE');
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch(async error => {
    const failure = { observedAt: new Date().toISOString(), name: error.name, message: error.message,
      httpStatus: error.response?.status, retryAfter: error.response?.headers?.['retry-after'], response: error.response?.data };
    await writeFile(new URL(`failure-${Date.now()}.json`, directory), json(failure), { mode: 0o600 });
    console.error({ name: failure.name, message: failure.message, httpStatus: failure.httpStatus, retryAfter: failure.retryAfter });
    process.exitCode = 1;
  });
}
