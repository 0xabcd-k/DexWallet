// Isolated Nile CLI. Platform setup is signed locally; user payments require a real wallet.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { open, readFile, writeFile, rename, unlink } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { post } from './rpc.mjs';
import { AMOUNT_SUN, CHAIN_ID, ENDPOINT, MAX_SETUP_FEE_SUN, STAKE_SUN, TOP_UP_STAKE_SUN, SETUP_MEMO, DELEGATE_SUN, tron, hex, headerOptions, checkTransfer, verifyWalletSignature, requireDelegatedBandwidth, verifyPayment } from './tron-live-core.mjs';

const mode = process.argv[2];
assert(process.argv.length === 3 && ['--setup', '--prepare', '--submit', '--audit', '--replay'].includes(mode));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const path = `${root}/tron-live-state.json`;
const addresses = JSON.parse(await readFile(`${root}/tron-live-addresses.json`, 'utf8'));
assert.deepEqual(addresses, { user: 'TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio', sponsor: 'TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH' });
const query = async (method, body = {}, solid = false) => {
  const value = await post(`${ENDPOINT}/${solid ? 'walletsolidity' : 'wallet'}/${method}`, body, { direct: true });
  assert(!value.Error, `RPC ${method}: ${value.Error}`); return value;
};
let state;
const save = async () => { await writeFile(`${path}.tmp`, JSON.stringify(state, null, 2) + '\n', { mode: 0o600 }); await rename(`${path}.tmp`, path); };
async function snapshot() {
  const pairs = await Promise.all(Object.entries(addresses).map(async ([role, address]) => [role, {
    account: await query('getaccount', { address, visible: true }),
    resources: await query('getaccountresource', { address, visible: true }),
  }]));
  return { observedAt: new Date().toISOString(), addresses, ...Object.fromEntries(pairs),
    delegation: await query('getdelegatedresourcev2', { fromAddress: hex(addresses.sponsor), toAddress: hex(addresses.user) }) };
}
async function confirmation(signed) {
  const info = await query('gettransactioninfobyid', { value: signed.txID }, true);
  if (!info.id) return null;
  const transaction = await query('gettransactionbyid', { value: signed.txID }, true);
  assert.equal(info.id, signed.txID); assert.equal(transaction.raw_data_hex, signed.raw_data_hex);
  assert.deepEqual(transaction.ret.map(result => result.contractRet), ['SUCCESS']);
  assert.notEqual(info.result, 'FAILED');
  return { info, transaction, status: 'solidified' };
}
async function waitConfirmation(signed) {
  for (const start = Date.now(); Date.now() - start < 110_000;) {
    const result = await confirmation(signed); if (result) return result;
    await new Promise(resolve => setTimeout(resolve, 3000));
  }
  throw new Error('Waiting for solidity-node confirmation; inspect the saved txID, never replace it blindly');
}
async function broadcast(entry) {
  const existing = await query('gettransactionbyid', { value: entry.signed.txID });
  if (existing.txID) return;
  assert(Date.now() < entry.signed.raw_data.expiration, 'Saved transaction expired; manual inspection required');
  entry.broadcast = await query('broadcasttransaction', entry.signed);
  await save();
  assert(entry.broadcast.result === true || entry.broadcast.code === 'DUP_TRANSACTION_ERROR', JSON.stringify(entry.broadcast));
}
async function setupStep(name, build) {
  if (state.setup[name]?.status === 'solidified') return;
  if (!state.setup[name]) {
    const before = await snapshot();
    if (name === 'activate') {
      assert(!before.user.account.address, 'Do not fund an already activated user');
      assert(before.sponsor.account.balance >= STAKE_SUN + MAX_SETUP_FEE_SUN + AMOUNT_SUN, 'Await test faucet funding');
    }
    if (name === 'delegate') {
      const max = await query('getcandelegatedmaxsize', { owner_address: hex(addresses.sponsor), type: 0 });
      assert(max.max_size >= DELEGATE_SUN, 'Platform does not have enough delegable bandwidth stake');
    }
    if (name === 'topUpStake') {
      assert(before.sponsor.account.balance >= TOP_UP_STAKE_SUN + MAX_SETUP_FEE_SUN);
    }
    const block = await query('getnowblock');
    const unsigned = await build(headerOptions(block));
    const key = JSON.parse(await readFile(`${root}/tron-sponsor-only.json`, 'utf8'));
    const signed = await tron.trx.sign(unsigned, key);
    assert.equal(tron.trx.ecRecover(signed), addresses.sponsor);
    state.setup[name] = { before, signed, txID: signed.txID, status: 'signed' }; await save();
  }
  const entry = state.setup[name];
  await broadcast(entry);
  Object.assign(entry, await waitConfirmation(entry.signed));
  assert((entry.info.fee ?? 0) <= MAX_SETUP_FEE_SUN);
  entry.after = await snapshot(); await save();
  console.log(JSON.stringify({ setup: name, txID: entry.txID, status: entry.status, feeSun: entry.info.fee ?? 0 }));
}
async function audit() {
  assert(state.payment, 'No submitted payment to audit');
  const confirmed = await confirmation(state.payment.signed);
  assert(confirmed, 'Payment has not been observed by the solidity node');
  // Preserve the initial post-payment resource evidence; quota continuously recovers.
  const after = state.payment.after ?? await snapshot();
  const checks = verifyPayment(state.order, state.payment.signed, confirmed.info, confirmed.transaction, state.payment.before, after);
  Object.assign(state.payment, confirmed, { after, checks }); await save();
  // Export only after consumption, when the payment transaction is already public onchain.
  await writeFile(`${root}/tron-live-public-result.json`, JSON.stringify({ observedAt: new Date().toISOString(),
    network: 'Nile', walletVersion: '4.11.2', addresses, amountSun: AMOUNT_SUN,
    stakeSun: STAKE_SUN + TOP_UP_STAKE_SUN, delegatedStakeSun: DELEGATE_SUN, setup: state.setup,
    payment: state.payment, replay: state.replay }, null, 2) + '\n');
}
const lock = await open(`${path}.lock`, 'wx', 0o600);
try {
  const chain = await post(`${ENDPOINT}/jsonrpc`, { jsonrpc: '2.0', id: 1, method: 'eth_chainId', params: [] }, { direct: true });
  assert.equal(chain.result, CHAIN_ID);
  try { state = JSON.parse(await readFile(path, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  state ??= { createdAt: new Date().toISOString(), addresses, setup: {} };
  assert.deepEqual(state.addresses, addresses);
  if (mode === '--setup') {
    await setupStep('activate', options => tron.transactionBuilder.sendTrx(addresses.user, AMOUNT_SUN, addresses.sponsor, options));
    await setupStep('stake', options => tron.transactionBuilder.freezeBalanceV2(STAKE_SUN, 'BANDWIDTH', addresses.sponsor, options));
    await setupStep('topUpStake', async options => {
      const unsigned = await tron.transactionBuilder.freezeBalanceV2(TOP_UP_STAKE_SUN, 'BANDWIDTH', addresses.sponsor, options);
      return tron.transactionBuilder.addUpdateData(unsigned, SETUP_MEMO, 'utf8', { txLocal: true });
    });
    await setupStep('delegate', options => tron.transactionBuilder.delegateResource(DELEGATE_SUN, addresses.user, 'BANDWIDTH', addresses.sponsor, false, undefined, options));
  } else if (mode === '--prepare') {
    assert(!state.order, 'Review existing order before requesting another wallet signature');
    assert.equal(state.setup.delegate?.status, 'solidified');
    const block = await query('getnowblock');
    const unsigned = await tron.transactionBuilder.sendTrx(addresses.sponsor, AMOUNT_SUN, addresses.user, headerOptions(block));
    checkTransfer(unsigned, addresses);
    state.order = { reference: randomUUID(), addresses, unsigned, preparedAt: new Date().toISOString() }; await save();
  } else if (mode === '--submit') {
    assert(state.order, 'Prepare a bounded transfer first');
    if (!state.payment) {
      const signed = JSON.parse(await readFile(`${root}/tron-live-wallet-signed.json`, 'utf8'));
      const block = await query('getnowblock');
      const signature = verifyWalletSignature(state.order, signed, block.block_header.raw_data.timestamp);
      const before = await snapshot();
      const resources = requireDelegatedBandwidth(before, signature.bandwidthEstimate);
      state.payment = { signed, txID: signed.txID, status: 'signed', before, signature, resources }; await save();
    }
    if (state.payment.status === 'signed') {
      // Resource delegation is not an atomic per-transaction reservation. Recheck immediately before broadcasting.
      requireDelegatedBandwidth(await snapshot(), state.payment.signature.bandwidthEstimate);
      await broadcast(state.payment); state.payment.status = 'submitted'; await save();
    }
    Object.assign(state.payment, await waitConfirmation(state.payment.signed)); await save(); await audit();
  } else if (mode === '--replay') {
    assert.equal(state.payment?.status, 'solidified');
    const before = await snapshot();
    const response = await query('broadcasttransaction', state.payment.signed);
    // Protobuf JSON omits the default false result on Nile error responses.
    assert(response.result === undefined || response.result === false);
    assert.equal(response.code, 'DUP_TRANSACTION_ERROR');
    const after = await snapshot();
    for (const role of ['user', 'sponsor']) assert.equal(after[role].account.balance ?? 0, before[role].account.balance ?? 0);
    state.replay = { observedAt: new Date().toISOString(), response, balancesUnchanged: true }; await save(); await audit();
  } else await audit();
  console.log(JSON.stringify({ mode, orderTxID: state.order?.unsigned.txID, payment: state.payment && { txID: state.payment.txID, status: state.payment.status, checks: state.payment.checks } }));
} finally { await lock.close(); await unlink(`${path}.lock`); }
