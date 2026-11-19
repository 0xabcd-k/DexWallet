// TESTNET-only preparation for the Go Jetton send executor. Reuses the dedicated
// V4 test key; immutable funding records must never be deleted to retry a send.
import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { Address, Cell, TonClient, JettonMaster, WalletContractV4, WalletContractV5R1, beginCell, internal, external, storeMessage } from '@ton/ton';
import { keyPairFromSeed, mnemonicToPrivateKey } from '@ton/crypto';
import { MASTER } from './ton-jetton.mjs';

const dir = new URL('../../.tools/ton-jetton-send/', import.meta.url);
const platform = Address.parse('0:100769e6dae579cfbc5ca1637c88d6c83a6c2195b3e571f1e35f20b45b2fa4d0');
const masterAddress = Address.parse(MASTER);
const masterCode = 'GNW254D/C7RRJUwsdg0J1uSFY4zRQHq7lweHUsPBye4=';
const tokenCode = 'j0Utek39dAZraCNlF3JZ7QVzRDW+drX9S9XYryt8PWg=';
const atoms = 100000n, attached = 50000000n, forward = 1n;
const client = new TonClient({ endpoint: 'https://testnet.toncenter.com/api/v2/jsonRPC', timeout: 20000 });
const json = x => JSON.stringify(x, (_, v) => typeof v === 'bigint' ? v.toString() : v, 2) + '\n';
const save = (name, x) => writeFile(new URL(name, dir), json(x), { mode: 0o600, flag: 'wx' });
let last = 0;
async function rpc(fn) {
  await new Promise(resolve => setTimeout(resolve, Math.max(0, 1500 - (Date.now() - last))));
  try { return await fn(); } finally { last = Date.now(); }
}
async function token(owner) {
  const address = await rpc(() => client.open(JettonMaster.create(masterAddress)).getWalletAddress(owner));
  const state = await rpc(() => client.getContractState(address));
  let balance = 0n, status = 0;
  if (state.state === 'active') {
    assert.equal(Cell.fromBoc(state.code)[0].hash().toString('base64'), tokenCode);
    status = Cell.fromBoc(state.data)[0].beginParse().loadUint(4);
    const { stack } = await rpc(() => client.runMethod(address, 'get_wallet_data'));
    balance = stack.readBigNumber(); assert(stack.readAddress().equals(owner));
    assert(stack.readAddress().equals(masterAddress)); assert.equal(stack.readCell().hash().toString('base64'), tokenCode);
  } else assert(['uninitialized', 'non-existing'].includes(state.state));
  return { address: address.toRawString(), state: state.state, status, balance: balance.toString(), tonBalance: state.balance.toString() };
}
async function report() {
  const proof = JSON.parse(await readFile(new URL('confirmed-evidence.json', dir), 'utf8'));
  const receipt = proof.delivery.receipt.transaction;
  const response = await fetch('https://testnet.toncenter.com/api/v3/adjacentTransactions?' + new URLSearchParams({ hash: receipt.signature, direction: 'out' }));
  assert(response.ok); const children = await response.json(); assert.equal(children.error, undefined); assert(Array.isArray(children.transactions)); last = Date.now();
  const head = await rpc(() => fetch('https://testnet.toncenter.com/api/v2/getMasterchainInfo').then(r => r.json()));
  assert.equal(head.ok, true); assert.equal(head.result.init.root_hash, 'gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg=');
  const receiver = proof.evidence.transaction.transactions[0];
  const normalHash = x => /^[0-9a-f]{64}$/i.test(x) ? x.toLowerCase() : Buffer.from(x, 'base64').toString('hex');
  assert.equal(children.transactions.length, receiver.out_msgs.length);
  const expected = new Map(receiver.out_msgs.map(m => [normalHash(m.hash), m]));
  for (const tx of children.transactions) {
    const message = expected.get(normalHash(tx.in_msg.hash)); assert(message, 'Unrelated downstream message');
    assert(Address.parse(tx.account).equals(Address.parse(message.destination)));
    assert.equal(tx.in_msg.value, message.value);
    assert.equal(normalHash(tx.in_msg.message_content.hash), normalHash(message.message_content.hash));
    assert.equal(normalHash(tx.trace_id), normalHash(receiver.trace_id));
    assert(BigInt(tx.lt) > BigInt(message.created_lt)); assert.equal(tx.out_msgs.length, 0);
    expected.delete(normalHash(tx.in_msg.hash));
  }
  assert.equal(expected.size, 0);
  const txs = [proof.evidence.dispatch.transactions[0], proof.evidence.parent.transactions[0], receiver, ...children.transactions];
  assert.equal(new Set(txs.map(tx => normalHash(tx.hash))).size, txs.length);
  const rows = txs.map(tx => {
    assert.equal(tx.finality, 'finalized'); assert.equal(tx.emulated, false); assert(tx.mc_block_seqno <= head.result.last.seqno);
    assert.equal(Object.keys(tx.total_fees_extra_currencies ?? {}).length, 0);
    const forwardFees = tx.out_msgs.reduce((sum, m) => sum + BigInt(m.fwd_fee), 0n);
    return { hash: normalHash(tx.hash), account: Address.parse(tx.account).toRawString(), totalFees: tx.total_fees, outgoingForwardFees: forwardFees.toString(), cost: (BigInt(tx.total_fees) + forwardFees).toString(), compute: tx.description.compute_ph, incomingValue: tx.in_msg.value };
  });
  const summary = { observedAt: new Date().toISOString(), network: 'ton-testnet', source: 'explicit trusted indexer observation, not fee settlement', rows, traceCostNanoton: rows.reduce((sum, tx) => sum + BigInt(tx.cost), 0n).toString(), children, head };
  await save('trace-cost.json', summary); console.log(json({ rows, traceCostNanoton: summary.traceCostNanoton }));
}
async function main() {
  const action = process.argv[2];
  if (action === 'report') { await report(); return; }
  assert(['inspect', 'fund', 'policy'].includes(action));
  await mkdir(dir, { recursive: true, mode: 0o700 });
  const seed = Buffer.from((await readFile(new URL('../../.tools/ton-live-send/v4-test.seed', import.meta.url), 'utf8')).trim(), 'hex');
  assert.equal(seed.length, 32);
  const key = keyPairFromSeed(seed), wallet = WalletContractV4.create({ publicKey: key.publicKey, workchain: 0 });
  const network = await fetch('https://testnet.toncenter.com/api/v2/getMasterchainInfo').then(r => r.json());
  assert.equal(network.ok, true); assert.equal(network.result.init.root_hash, 'gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg='); last = Date.now();
  const ms = await rpc(() => client.getContractState(masterAddress));
  assert.equal(Cell.fromBoc(ms.code)[0].hash().toString('base64'), masterCode);
  const state = await rpc(() => client.getContractState(wallet.address));
  assert.equal(state.state, 'active'); assert(Cell.fromBoc(state.code)[0].equals(wallet.init.code));
  const data = Cell.fromBoc(state.data)[0].beginParse(), seqno = data.loadUint(32);
  assert.equal(data.loadUint(32), wallet.walletId); assert(data.loadBuffer(32).equals(key.publicKey)); assert.equal(data.loadBit(), false);
  const senderToken = await token(wallet.address), platformToken = await token(platform);
  const snapshot = { observedAt: new Date().toISOString(), network: 'ton-testnet', master: masterAddress.toRawString(), masterCode, tokenCode, sender: wallet.address.toRawString(), publicKey: key.publicKey.toString('base64'), seqno, balance: state.balance.toString(), senderToken, recipient: platform.toRawString(), platformToken };
  await save(`state-${Date.now()}.json`, snapshot);
  if (action === 'inspect') { console.log(json(snapshot)); return; }
  assert.equal(senderToken.status & 1, 0); assert.equal(platformToken.status & 2, 0);
  if (action === 'policy') {
    assert(BigInt(senderToken.balance) >= atoms); assert(state.balance > attached + 10000000n);
    const policy = { wallet: { PublicKey: key.publicKey.toString('base64'), SubwalletID: wallet.walletId, JettonMaster: masterAddress.toRawString() }, proposal: { key: 'ton-jetton-live-20261119-01', recipient: platform.toRawString(), atoms: atoms.toString(), seqno, validUntil: Math.floor(Date.now() / 1000) + 600, jetton: { master: masterAddress.toRawString(), wallet: senderToken.address, attachTon: attached.toString(), forwardTon: forward.toString(), queryId: '2026111901' } } };
    await save('policy.json', policy); console.log(json(policy)); return;
  }
  assert.equal(senderToken.balance, '0', 'Already funded; inspect original funding instead');
  assert(BigInt(platformToken.balance) >= atoms); assert.equal(platformToken.status & 1, 0); assert.equal(senderToken.status & 2, 0);
  const phrase = (await readFile(new URL('../../.tools/payment-feasibility/ton-acton/platform-phrase.txt', import.meta.url), 'utf8')).trim().split(/\s+/);
  const treasuryKey = await mnemonicToPrivateKey(phrase);
  const treasury = WalletContractV5R1.create({ publicKey: treasuryKey.publicKey, walletId: { networkGlobalId: -3 } });
  assert(treasury.address.equals(platform));
  const ts = await rpc(() => client.getContractState(platform));
  assert.equal(ts.state, 'active'); assert(Cell.fromBoc(ts.code)[0].equals(treasury.init.code)); assert(ts.balance > attached + 10000000n);
  const td = Cell.fromBoc(ts.data)[0].beginParse(); assert(td.loadBit()); const tsSeqno = td.loadUint(32);
  td.loadUint(32); assert(td.loadBuffer(32).equals(treasuryKey.publicKey)); assert.equal(td.loadBit(), false);
  const transfer = beginCell().storeUint(0x0f8a7ea5, 32).storeUint(2026111900n, 64).storeCoins(atoms).storeAddress(wallet.address).storeAddress(platform).storeBit(false).storeCoins(forward).storeBit(false).endCell();
  const validUntil = Math.floor(Date.now() / 1000) + 180;
  const body = treasury.createTransfer({ seqno: tsSeqno, secretKey: treasuryKey.secretKey, timeout: validUntil, sendMode: 3, messages: [internal({ to: platformToken.address, value: attached, bounce: true, body: transfer })] });
  const wire = beginCell().store(storeMessage(external({ to: treasury.address, body }))).endCell();
  const funding = { role: 'test-fixture-funding', snapshot, sender: platform.toRawString(), recipient: wallet.address.toRawString(), atoms: atoms.toString(), queryId: '2026111900', seqno: tsSeqno, validUntil, bodyHash: body.hash().toString('hex'), messageHash: wire.hash().toString('hex') };
  await save('funding.json', funding); await writeFile(new URL('funding.boc', dir), wire.toBoc(), { mode: 0o600, flag: 'wx' });
  await rpc(() => client.sendFile(wire.toBoc())); console.log(json({ ...funding, status: 'node-accepted-needs-verification' }));
}
main().catch(e => { console.error(e.message); process.exitCode = 1; });
