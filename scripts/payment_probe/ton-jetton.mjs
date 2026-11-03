import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { Address, Cell, TonClient, WalletContractV5R1, JettonMaster, beginCell, internal, external, storeMessage, storeMessageRelaxed, loadMessageRelaxed, loadStateInit, contractAddress } from '@ton/ton';
import { mnemonicToPrivateKey, signVerify } from '@ton/crypto';
import { loadOutListExtendedV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1Actions.js';
import { storeWalletIdV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1WalletId.js';
import { USER, PLATFORM, PUBLIC_KEY } from './ton-live.mjs';

// Public community test token from ton-community/tma-usdt-payments-demo.
// Fixed testnet accounts and amounts; never accepts a mainnet RPC or arbitrary target.
export const MASTER = 'kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy';
const GIVER = '0:cd503242d2242f268669d2697e62d78bd4db535caa61534dc24c8e6fe15b4184';
const MASTER_CODE = 'GNW254D/C7RRJUwsdg0J1uSFY4zRQHq7lweHUsPBye4=';
const TOKEN_CODE = 'j0Utek39dAZraCNlF3JZ7QVzRDW+drX9S9XYryt8PWg=';
const OP = { claim: 0x29577499, transfer: 0x0f8a7ea5 };
const TOKEN_AMOUNT = 1_000_000n; // 1 test token, six decimals.
const CLAIM_AMOUNT = 10_000_000n; // 10 test tokens, observed public giver format.
export const directory = new URL('../../.tools/payment-feasibility/ton-jetton/', import.meta.url);
const client = new TonClient({ endpoint: 'https://testnet.toncenter.com/api/v2/jsonRPC', timeout: 20000 });
const userWallet = WalletContractV5R1.create({ publicKey: Buffer.from(PUBLIC_KEY, 'hex'), walletId: { networkGlobalId: -3 } });
const walletId = beginCell().store(storeWalletIdV5R1(userWallet.walletId)).endCell().beginParse().loadUint(32);
const json = value => JSON.stringify(value, (_key, v) => typeof v === 'bigint' ? v.toString() : v, 2) + '\n';
export const save = (name, value, exclusive = false) => writeFile(new URL(name, directory), json(value), { mode: 0o600, flag: exclusive ? 'wx' : 'w' });
export const read = async name => JSON.parse(await readFile(new URL(name, directory), 'utf8'));
let lastRpc = 0;
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function rpc(fn) {
  await pause(Math.max(0, 3000 - (Date.now() - lastRpc)));
  try { return await fn(); } finally { lastRpc = Date.now(); }
}
export async function state() {
  const master = client.open(JettonMaster.create(Address.parse(MASTER)));
  const data = await rpc(() => master.getJettonData());
  assert.equal(data.adminAddress.toRawString(), GIVER);
  assert.equal(data.walletCode.hash().toString('base64'), TOKEN_CODE);
  const ms = await rpc(() => client.getContractState(Address.parse(MASTER)));
  assert.equal(Cell.fromBoc(ms.code)[0].hash().toString('base64'), MASTER_CODE);
  const result = { observedAt: new Date().toISOString(), master: MASTER, masterCodeHash: MASTER_CODE,
    tokenCodeHash: TOKEN_CODE, totalSupply: data.totalSupply.toString(), accounts: {} };
  for (const [role, raw] of [['platform', PLATFORM], ['user', USER]]) {
    const address = Address.parse(raw);
    const s = await rpc(() => client.getContractState(address));
    assert.equal(s.state, 'active');
    assert(Cell.fromBoc(s.code)[0].equals(userWallet.init.code));
    const d = Cell.fromBoc(s.data)[0].beginParse();
    assert(d.loadBoolean());
    const seqno = d.loadUint(32); assert.equal(d.loadUint(32), walletId);
    const publicKey = d.loadBuffer(32).toString('hex');
    if (role === 'user') assert.equal(publicKey, PUBLIC_KEY);
    assert.equal(d.loadBoolean(), false, 'Unexpected wallet extensions');
    const tokenAddress = await rpc(() => master.getWalletAddress(address));
    const ts = await rpc(() => client.getContractState(tokenAddress));
    let tokenBalance = '0', tokenStatus = null;
    if (ts.state === 'active') {
      assert.equal(Cell.fromBoc(ts.code)[0].hash().toString('base64'), TOKEN_CODE);
      tokenStatus = Cell.fromBoc(ts.data)[0].beginParse().loadUint(4);
      const { stack } = await rpc(() => client.runMethod(tokenAddress, 'get_wallet_data'));
      tokenBalance = stack.readBigNumber().toString();
      assert(stack.readAddress().equals(address));
      assert(stack.readAddress().equals(Address.parse(MASTER)));
      assert.equal(stack.readCell().hash().toString('base64'), TOKEN_CODE);
    } else assert(['uninitialized', 'non-existing'].includes(ts.state));
    result.accounts[role] = { address: raw, publicKey, state: s.state, seqno, balance: s.balance.toString(), lastTransaction: s.lastTransaction,
      token: { address: tokenAddress.toRawString(), state: ts.state, status: tokenStatus, balance: tokenBalance, tonBalance: ts.balance.toString(), lastTransaction: ts.lastTransaction } };
  }
  return result;
}
export function plan(mode, snapshot) {
  assert(['claim', 'payment'].includes(mode));
  const queryId = mode === 'claim' ? 0n : 2026110301n;
  const payload = mode === 'claim'
    ? beginCell().storeUint(OP.claim, 32).storeUint(queryId, 64).storeUint(CLAIM_AMOUNT, 64).endCell()
    : beginCell().storeUint(OP.transfer, 32).storeUint(queryId, 64).storeCoins(TOKEN_AMOUNT)
      .storeAddress(Address.parse(PLATFORM)).storeAddress(Address.parse(PLATFORM))
      .storeBit(false).storeCoins(1n).storeBit(false).endCell();
  const amount = mode === 'claim' ? 49_000_000n : 50_000_000n;
  const target = mode === 'claim' ? GIVER : snapshot.accounts.user.token.address;
  return { mode, queryId: queryId.toString(), target, amount: amount.toString(), relayValue: (amount + 3_000_000n).toString(),
    payload: payload.toBoc().toString('base64'), payloadHash: payload.hash().toString('hex'), tokenAmount: (mode === 'claim' ? CLAIM_AMOUNT : TOKEN_AMOUNT).toString() };
}
export function validate(capture, expected, seqno, now = Math.floor(Date.now() / 1000)) {
  assert.equal(capture.account.chain, '-3'); assert.equal(capture.request.network, '-3');
  assert.equal(capture.account.address, USER); assert.equal(capture.account.publicKey, PUBLIC_KEY);
  assert.equal(capture.mode, expected.mode);
  assert(contractAddress(0, loadStateInit(Cell.fromBase64(capture.account.walletStateInit).beginParse())).equals(userWallet.address));
  assert.equal(capture.request.messages.length, 1);
  const requested = capture.request.messages[0];
  assert(Address.parse(requested.address).equals(Address.parse(expected.target)));
  assert.equal(requested.amount, expected.amount);
  assert(Cell.fromBase64(requested.payload).equals(Cell.fromBase64(expected.payload)), 'Requested token payload mismatch');
  const wrapper = loadMessageRelaxed(Cell.fromBase64(capture.result.internalBoc).beginParse());
  assert.equal(wrapper.info.type, 'internal'); assert(wrapper.info.dest.equals(userWallet.address));
  assert.equal(wrapper.info.src, null); assert.equal(wrapper.info.value.coins, 0n);
  assert.equal(wrapper.init, null); assert.equal(wrapper.info.bounced, false);
  const s = wrapper.body.beginParse(), unsignedBuilder = beginCell().storeBits(s.loadBits(s.remainingBits - 512));
  const signature = s.loadBuffer(64); while (s.remainingRefs) unsignedBuilder.storeRef(s.loadRef());
  const unsigned = unsignedBuilder.endCell();
  assert(signVerify(unsigned.hash(), signature, Buffer.from(PUBLIC_KEY, 'hex')), 'Invalid wallet signature');
  const f = unsigned.beginParse();
  assert.equal(f.loadUint(32), WalletContractV5R1.OpCodes.auth_signed_internal);
  assert.equal(f.loadUint(32), walletId);
  const validUntil = f.loadUint(32); assert.equal(f.loadUint(32), seqno, 'Stale seqno'); assert(seqno > 0);
  assert.equal(validUntil, capture.request.validUntil);
  assert(validUntil > now + 30 && validUntil <= now + 300, 'Expired or excessive deadline');
  const actions = loadOutListExtendedV5R1(f); assert.equal(f.remainingBits + f.remainingRefs, 0);
  assert.equal(actions.length, 1); const action = actions[0];
  assert.equal(action.type, 'sendMsg'); assert.equal(action.mode, 3);
  const m = action.outMsg; assert.equal(m.info.type, 'internal');
  assert(m.info.dest.equals(Address.parse(expected.target)));
  assert.equal(m.info.value.coins.toString(), expected.amount);
  assert(!m.info.value.other || m.info.value.other.size === 0); assert.equal(m.init, null);
  assert(m.body.equals(Cell.fromBase64(expected.payload)), 'Signed token payload mismatch');
  return { body: wrapper.body, bodyHash: wrapper.body.hash().toString('hex'), validUntil, seqno };
}
function audit(capture, expected, seqno) {
  const now = Math.floor(Date.now() / 1000), auth = validate(capture, expected, seqno, now), rejected = [];
  for (const [name, mutate] of [
    ['wrong network', c => { c.account.chain = '-239'; }],
    ['changed attached TON', c => { c.request.messages[0].amount = '1'; }],
    ['changed token wallet', c => { c.request.messages[0].address = PLATFORM; }],
    ['changed token payload', c => { c.request.messages[0].payload = beginCell().storeUint(0, 32).endCell().toBoc().toString('base64'); }],
    ['changed deadline', c => { c.request.validUntil -= 1; }],
  ]) { const copy = structuredClone(capture); mutate(copy); assert.throws(() => validate(copy, expected, seqno, now)); rejected.push(name); }
  const tampered = structuredClone(capture);
  const message = loadMessageRelaxed(Cell.fromBase64(tampered.result.internalBoc).beginParse());
  const slice = message.body.beginParse(); message.body = beginCell().storeUint(slice.loadUint(32) ^ 1, 32).storeSlice(slice).endCell();
  tampered.result.internalBoc = beginCell().store(storeMessageRelaxed(message)).endCell().toBoc().toString('base64');
  assert.throws(() => validate(tampered, expected, seqno, now), /signature/); rejected.push('modified signed body');
  if (expected.mode === 'payment') {
    for (const [name, amount, recipient, response, forward] of [
      ['changed token amount', TOKEN_AMOUNT + 1n, PLATFORM, PLATFORM, 1n],
      ['changed token recipient', TOKEN_AMOUNT, USER, PLATFORM, 1n],
      ['changed excess destination', TOKEN_AMOUNT, PLATFORM, USER, 1n],
      ['changed forwarded TON', TOKEN_AMOUNT, PLATFORM, PLATFORM, 2n],
    ]) {
      const copy = structuredClone(capture);
      copy.request.messages[0].payload = beginCell().storeUint(OP.transfer, 32).storeUint(BigInt(expected.queryId), 64).storeCoins(amount)
        .storeAddress(Address.parse(recipient)).storeAddress(Address.parse(response)).storeBit(false).storeCoins(forward).storeBit(false).endCell().toBoc().toString('base64');
      assert.throws(() => validate(copy, expected, seqno, now), /payload/); rejected.push(name);
    }
  }
  assert.throws(() => validate(capture, expected, seqno + 1, now), /seqno/); rejected.push('stale seqno');
  assert.throws(() => validate(capture, expected, seqno, auth.validUntil + 1), /deadline/); rejected.push('expired authorization');
  return { auth, report: { observedAt: new Date().toISOString(), mode: expected.mode, wallet: capture.device,
    seqno, validUntil: auth.validUntil, bodyHash: auth.bodyHash, signatureVerified: true, rejected } };
}
async function relay(mode, filename) {
  const before = await state(), expected = plan(mode, before);
  if (mode === 'claim') assert.equal(before.accounts.user.token.balance, '0', 'Already funded; do not claim again');
  else {
    assert(BigInt(before.accounts.user.token.balance) >= TOKEN_AMOUNT);
    assert.equal(before.accounts.user.token.status & 1, 0, 'Sender token wallet locked');
    assert.equal((before.accounts.platform.token.status ?? 0) & 2, 0, 'Recipient token wallet locked');
  }
  const capture = JSON.parse(await readFile(filename, 'utf8'));
  const { auth, report } = audit(capture, expected, before.accounts.user.seqno);
  await save(`${mode}-preflight-${Date.now()}.json`, report, true);
  const phrase = (await readFile(new URL('../ton-acton/platform-phrase.txt', directory), 'utf8')).trim().split(/\s+/);
  const key = await mnemonicToPrivateKey(phrase);
  const wallet = WalletContractV5R1.create({ publicKey: key.publicKey, walletId: { networkGlobalId: -3 } });
  assert.equal(wallet.address.toRawString(), PLATFORM);
  assert(BigInt(before.accounts.platform.balance) > BigInt(expected.relayValue) + 10_000_000n);
  const validUntil = Math.min(Math.floor(Date.now() / 1000) + 120, auth.validUntil);
  const body = wallet.createTransfer({ seqno: before.accounts.platform.seqno, secretKey: key.secretKey, timeout: validUntil, sendMode: 3,
    messages: [internal({ to: USER, value: BigInt(expected.relayValue), bounce: true, body: auth.body })] });
  const wire = beginCell().store(storeMessage(external({ to: wallet.address, body }))).endCell();
  const record = { observedAt: new Date().toISOString(), mode, before, expected, authorization: { bodyHash: auth.bodyHash, seqno: auth.seqno, validUntil: auth.validUntil },
    externalHash: wire.hash().toString('hex'), platformBodyHash: body.hash().toString('hex'), externalValidUntil: validUntil, status: 'prepared' };
  await save(`${mode}-relay.json`, record, true);
  await writeFile(new URL(`${mode}-wire.boc`, directory), wire.toBoc(), { mode: 0o600, flag: 'wx' });
  await rpc(() => { assert(validUntil > Date.now() / 1000 + 15); return client.sendFile(wire.toBoc()); });
  record.status = 'broadcast-accepted'; await save(`${mode}-relay.json`, record);
  console.log(json({ mode, status: record.status, externalHash: record.externalHash }));
  await pause(5000);
  const after = await state(); record.after = after; record.status = 'needs-transaction-trace';
  await save(`${mode}-relay.json`, record); console.log(json(record));
}
function summary(m) {
  if (!m) return null;
  const s = m.body.beginParse();
  const opcode = s.remainingBits >= 32 ? s.loadUint(32) : null;
  const hasQueryId = [OP.claim, OP.transfer, 0x178d4519, 0x7362d09c, 0xd53276db, 0x642b7d07].includes(opcode);
  const queryId = hasQueryId && s.remainingBits >= 64 ? s.loadUintBig(64).toString() : null;
  let token;
  if ([OP.transfer, 0x178d4519, 0x7362d09c].includes(opcode)) {
    token = { amount: s.loadCoins().toString(), owner: s.loadAddress().toRawString() };
    if (opcode !== 0x7362d09c) {
      token.responseDestination = s.loadMaybeAddress()?.toRawString();
      if (opcode === OP.transfer) s.loadMaybeRef();
      token.forwardTon = s.loadCoins().toString();
    }
  }
  return { type: m.info.type, source: m.info.src?.toString(), destination: m.info.dest?.toString(),
    value: m.info.type === 'internal' ? m.info.value.coins : null, forwardFee: m.info.type === 'internal' ? m.info.forwardFee : null,
    bounced: m.info.type === 'internal' ? m.info.bounced : null, bodyHash: m.body.hash().toString('hex'),
    opcode: opcode === null ? null : `0x${opcode.toString(16).padStart(8, '0')}`, queryId, token };
}
async function report(mode) {
  const snapshot = await state(), record = await read(`${mode}-relay.json`), transactions = {};
  const accounts = { platform: PLATFORM, user: USER, userToken: snapshot.accounts.user.token.address, platformToken: snapshot.accounts.platform.token.address };
  if (mode === 'claim') Object.assign(accounts, { master: MASTER, giver: GIVER });
  for (const [role, raw] of Object.entries(accounts)) {
    const txs = await rpc(() => client.getTransactions(Address.parse(raw), { limit: 10 }));
    transactions[role] = txs.filter(t => t.now >= Math.floor(Date.parse(record.observedAt) / 1000) - 2 && t.now <= Math.floor(Date.parse(record.after.observedAt) / 1000)).map(t => ({ hash: t.hash().toString('hex'), lt: t.lt, now: t.now,
      oldStatus: t.oldStatus, endStatus: t.endStatus, totalFees: t.totalFees.coins, description: t.description,
      incoming: summary(t.inMessage), outgoing: [...t.outMessages.values()].map(summary) }));
  }
  const result = { observedAt: new Date().toISOString(), mode, snapshot, transactions };
  await save(`${mode}-chain-report.json`, result); console.log(json(result));
}
async function replay(mode) {
  assert.equal(mode, 'payment');
  const record = await read(`${mode}-relay.json`), wire = await readFile(new URL(`${mode}-wire.boc`, directory));
  assert.equal(Cell.fromBoc(wire)[0].hash().toString('hex'), record.externalHash);
  const accounts = { platform: PLATFORM, user: USER, userToken: record.before.accounts.user.token.address, platformToken: record.before.accounts.platform.token.address };
  async function quickState() {
    const result = { observedAt: new Date().toISOString(), accounts: {} };
    for (const [role, raw] of Object.entries(accounts)) {
      const s = await rpc(() => client.getContractState(Address.parse(raw))); assert.equal(s.state, 'active');
      const d = Cell.fromBoc(s.data)[0].beginParse();
      const token = role.endsWith('Token'); d.skip(token ? 4 : 1);
      result.accounts[role] = { balance: s.balance.toString(), lastTransaction: s.lastTransaction,
        ...(token ? { tokenBalance: d.loadCoins().toString() } : { seqno: d.loadUint(32) }) };
    }
    return result;
  }
  const before = await quickState();
  for (const role of ['platform', 'user']) assert.equal(before.accounts[role].seqno, record.before.accounts[role].seqno + 1);
  assert(record.externalValidUntil > Date.now() / 1000 + 20, 'Too late for an unexpired replay test');
  let rejection;
  try { await rpc(() => client.sendFile(wire)); }
  catch (error) { rejection = { name: error.name, message: error.message, status: error.response?.status, response: error.response?.data }; }
  await save('payment-replay-rpc-private.json', rejection ?? { accepted: true });
  const text = JSON.stringify(rejection), exitCode = text?.match(/(?:exitcode|exit_code|exit code)[^\d]{0,6}(\d+)/i)?.[1];
  assert.equal(exitCode, '133', 'Replay was not rejected with the expected chain error');
  await pause(5000); const after = await quickState();
  assert.deepEqual(after.accounts, before.accounts);
  const result = { observedAt: new Date().toISOString(), externalHash: record.externalHash, externalValidUntil: record.externalValidUntil,
    exitCode, before, after, balancesSeqnosAndTransactionsUnchanged: true };
  await save('payment-replay.json', result); console.log(json(result));
}
async function main() {
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const [command, mode, capture] = process.argv.slice(2);
  if (command === 'state') { const s = await state(); await save(`state-${Date.now()}.json`, s, true); console.log(json(s)); }
  else if (command === 'relay') await relay(mode, capture);
  else if (command === 'report') await report(mode);
  else if (command === 'replay') await replay(mode);
  else throw new Error('Usage: ton-jetton.mjs state|relay MODE CAPTURE|report MODE|replay payment');
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main().catch(async error => {
  await save(`failure-${Date.now()}.json`, { name: error.name, message: error.message, httpStatus: error.response?.status, response: error.response?.data });
  console.error({ name: error.name, message: error.message, status: error.response?.status }); process.exitCode = 1;
});
