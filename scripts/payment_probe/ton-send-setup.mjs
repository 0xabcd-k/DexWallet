// Explicit TESTNET preparation for the Go TON send probe. No mainnet support.
// Bootstrap records/BoCs are durable before submission; never delete to retry.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { Address, Cell, TonClient, WalletContractV4, WalletContractV5R1, beginCell, internal, external, storeMessage } from '@ton/ton';
import { keyPairFromSeed, mnemonicToPrivateKey } from '@ton/crypto';

const dir = new URL('../../.tools/ton-live-send/', import.meta.url);
const platform = Address.parse('0:100769e6dae579cfbc5ca1637c88d6c83a6c2195b3e571f1e35f20b45b2fa4d0');
const endpoint = 'https://testnet.toncenter.com/api/v2/jsonRPC';
const client = new TonClient({ endpoint, timeout: 20000 });
const stringify = x => JSON.stringify(x, (_, v) => typeof v === 'bigint' ? v.toString() : v, 2) + '\n';
let last = 0;
async function rpc(fn) {
  await new Promise(resolve => setTimeout(resolve, Math.max(0, 1500 - (Date.now() - last))));
  try { return await fn(); } finally { last = Date.now(); }
}
async function network() {
  const r = await fetch('https://testnet.toncenter.com/api/v2/getMasterchainInfo').then(r => r.json());
  assert.equal(r.ok, true); assert.equal(r.result.init.root_hash, 'gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg=');
  last = Date.now();
}
async function save(name, value) { await writeFile(new URL(name, dir), stringify(value), { mode: 0o600, flag: 'wx' }); }
async function submit(name, wallet, key, seqno, initialized, amount, target) {
  const body = wallet.createTransfer({ seqno, secretKey: key.secretKey, timeout: Math.floor(Date.now() / 1000) + 180, sendMode: 3, messages: [internal({ to: target, value: amount, bounce: false })] });
  const message = beginCell().store(storeMessage(external({ to: wallet.address, init: initialized ? null : wallet.init, body }))).endCell();
  const meta = { network: 'ton-testnet', role: 'test-wallet-bootstrap', sender: wallet.address.toRawString(), recipient: target.toRawString(), atoms: amount.toString(), seqno, messageHash: message.hash().toString('hex'), bodyHash: body.hash().toString('hex'), createdAt: new Date().toISOString() };
  await save(`${name}.json`, meta);
  await writeFile(new URL(`${name}.boc`, dir), message.toBoc(), { mode: 0o600, flag: 'wx' });
  await rpc(() => client.sendFile(message.toBoc()));
  console.log(stringify({ ...meta, status: 'node-accepted-needs-verification' }));
}
async function main() {
  await mkdir(dir, { recursive: true, mode: 0o700 });
  const action = process.argv[2];
  if (action === 'key') {
    await writeFile(new URL('v4-test.seed', dir), randomBytes(32).toString('hex') + '\n', { mode: 0o600, flag: 'wx' });
  }
  const seed = Buffer.from((await readFile(new URL('v4-test.seed', dir), 'utf8')).trim(), 'hex');
  assert.equal(seed.length, 32);
  const key = keyPairFromSeed(seed);
  const wallet = WalletContractV4.create({ publicKey: key.publicKey, workchain: 0 });
  if (action === 'key') { console.log(stringify({ address: wallet.address.toRawString(), publicKey: key.publicKey.toString('hex'), walletId: wallet.walletId })); return; }
  await network();
  const state = await rpc(() => client.getContractState(wallet.address));
  let seqno = 0;
  if (state.state === 'active') {
    assert(Cell.fromBoc(state.code)[0].equals(wallet.init.code), 'Unexpected V4 code');
    const data = Cell.fromBoc(state.data)[0].beginParse(); seqno = data.loadUint(32);
    assert.equal(data.loadUint(32), wallet.walletId);
    assert(data.loadBuffer(32).equals(key.publicKey));
    assert.equal(data.loadBit(), false, 'Unexpected V4 plugins');
  }
  if (action === 'inspect') {
    const treasury = await rpc(() => client.getContractState(platform));
    console.log(stringify({ address: wallet.address.toRawString(), state: state.state, seqno, balance: state.balance.toString(), lastTransaction: state.lastTransaction, platform: { balance: treasury.balance.toString(), lastTransaction: treasury.lastTransaction } }));
  } else if (action === 'fund') {
    assert.equal(state.balance, 0n, 'Already funded: inspect original bootstrap');
    const phrase = (await readFile(new URL('../../.tools/payment-feasibility/ton-acton/platform-phrase.txt', import.meta.url), 'utf8')).trim().split(/\s+/);
    const treasuryKey = await mnemonicToPrivateKey(phrase);
    const treasuryWallet = WalletContractV5R1.create({ publicKey: treasuryKey.publicKey, walletId: { networkGlobalId: -3 } });
    assert(treasuryWallet.address.equals(platform));
    const treasury = await rpc(() => client.getContractState(platform));
    assert.equal(treasury.state, 'active'); assert(treasury.balance > 150000000n);
    assert(Cell.fromBoc(treasury.code)[0].equals(treasuryWallet.init.code));
    const data = Cell.fromBoc(treasury.data)[0].beginParse(); assert(data.loadBit());
    await submit('bootstrap-fund', treasuryWallet, treasuryKey, data.loadUint(32), true, 100000000n, wallet.address);
  } else if (action === 'initialize') {
    assert.equal(state.state, 'uninitialized'); assert(state.balance >= 50000000n);
    await submit('bootstrap-init', wallet, key, 0, false, 1000000n, platform);
  } else if (action === 'policy') {
    assert.equal(state.state, 'active'); assert(seqno > 0); assert(state.balance >= 20000000n);
    const policy = { wallet: { PublicKey: key.publicKey.toString('base64'), SubwalletID: wallet.walletId }, proposal: { key: 'ton-live-20261119-01', recipient: platform.toRawString(), atoms: '10000000', seqno, validUntil: Math.floor(Date.now() / 1000) + 600 } };
    await save('policy.json', policy); console.log(stringify(policy));
  } else { throw new Error('Use key, inspect, fund, initialize or policy'); }
}
main().catch(e => { console.error(e.message); process.exitCode = 1; });
