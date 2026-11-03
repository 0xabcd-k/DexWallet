// Dedicated Nile resource experiment. Platform key only; user signs in TronLink.
import assert from 'node:assert/strict';
import { open, readFile, writeFile, rename, unlink } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { utils } from 'tronweb';
import { post, rpc } from './rpc.mjs';
import { tron, hex, ENDPOINT, CHAIN_ID, RESULT_BANDWIDTH, headerOptions } from './tron-live-core.mjs';
import { INIT_CODE, RUNTIME, ABI, CALL_DATA, FEE_LIMIT, STAKE_SUN, checkCall, checkResources, checkReceipt, verifySignature } from './tron-energy-core.mjs';

const mode = process.argv[2];
assert(process.argv.length === 3 && ['--setup', '--prepare', '--fund-bandwidth', '--submit', '--audit'].includes(mode));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const path = root + 'tron-energy-state.json';
const addresses = JSON.parse(await readFile(root + 'tron-live-addresses.json', 'utf8'));
assert.deepEqual(addresses, { user: 'TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio', sponsor: 'TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH' });
const query = async (method, body = {}, solid = false) => {
  const r = await post(`${ENDPOINT}/${solid ? 'walletsolidity' : 'wallet'}/${method}`, body, { direct: true });
  assert(!r.Error, `RPC ${method}: ${r.Error}`); return r;
};
let state;
const save = async () => { await writeFile(path + '.tmp', JSON.stringify(state, null, 2) + '\n', { mode: 0o600 }); await rename(path + '.tmp', path); };
async function snapshot() {
  const result = { observedAt: new Date().toISOString(), addresses };
  for (const [role, address] of Object.entries(addresses)) result[role] = {
    account: await query('getaccount', { address, visible: true }), resources: await query('getaccountresource', { address, visible: true }),
  };
  result.delegation = await query('getdelegatedresourcev2', { fromAddress: hex(addresses.sponsor), toAddress: hex(addresses.user) });
  return result;
}
async function confirmation(entry) {
  const info = await query('gettransactioninfobyid', { value: entry.signed.txID }, true);
  if (!info.id) return false;
  const tx = await query('gettransactionbyid', { value: entry.signed.txID }, true);
  assert.equal(tx.txID, entry.signed.txID); assert.equal(tx.raw_data_hex, entry.signed.raw_data_hex);
  assert.deepEqual(tx.ret.map(x => x.contractRet), ['SUCCESS']); assert.notEqual(info.result, 'FAILED');
  Object.assign(entry, { info, transaction: tx, status: 'solidified' }); return true;
}
async function submit(entry) {
  if (!(await query('gettransactionbyid', { value: entry.signed.txID })).txID) {
    assert(Date.now() < entry.signed.raw_data.expiration, 'Saved transaction expired; inspect before replacement');
    entry.broadcast = await query('broadcasttransaction', entry.signed); await save();
    assert(entry.broadcast.result === true || entry.broadcast.code === 'DUP_TRANSACTION_ERROR', JSON.stringify(entry.broadcast));
  }
  for (const start = Date.now(); Date.now() - start < 110_000;) {
    if (await confirmation(entry)) { await save(); return; }
    await new Promise(r => setTimeout(r, 3000));
  }
  throw new Error('Awaiting solidification; resume using the saved txID');
}
async function setupStep(name, build) {
  if (state.setup[name]?.status === 'solidified') return;
  if (!state.setup[name]) {
    const before = await snapshot();
    assert(before.sponsor.account.balance >= 3_000_000, 'Keep a bounded test fee reserve');
    const unsigned = await build(headerOptions(await query('getnowblock')));
    const key = JSON.parse(await readFile(root + 'tron-sponsor-only.json', 'utf8'));
    const signed = await tron.trx.sign(unsigned, key);
    assert.equal(tron.trx.ecRecover(signed), addresses.sponsor);
    state.setup[name] = { signed, before, status: 'signed' }; await save();
  }
  const entry = state.setup[name]; await submit(entry);
  assert((entry.info.fee ?? 0) <= 2_000_000);
  entry.after = await snapshot(); await save();
  console.log(JSON.stringify({ step: name, txID: entry.signed.txID, feeSun: entry.info.fee ?? 0 }));
}
async function audit() {
  assert(state.call, 'No signed call');
  assert(await confirmation(state.call), 'Call is not solidified; audit never broadcasts');
  state.call.after ??= await snapshot();
  checkCall(state.call.transaction, addresses, state.contractAddress);
  state.call.checks = checkReceipt(state.call.info, state.call.before, state.call.after, state.order.energy, state.order.bandwidthFeeSun);
  await save();
  await writeFile(root + 'tron-energy-public-result.json', JSON.stringify({ observedAt: new Date().toISOString(),
    network: 'Nile', chainId: CHAIN_ID, addresses, contractAddress: state.contractAddress, runtime: RUNTIME,
    contractSettings: state.order.contractSettings, simulation: state.order.simulation, setup: state.setup, call: state.call,
    scope: 'Energy mechanism only; no token transfer and no USDT fee sizing claim' }, null, 2) + '\n');
}
const lock = await open(path + '.lock', 'wx', 0o600);
try {
  assert.equal(await rpc(ENDPOINT + '/jsonrpc', 'eth_chainId', [], { direct: true }), CHAIN_ID);
  try { state = JSON.parse(await readFile(path, 'utf8')); } catch (e) { if (e.code !== 'ENOENT') throw e; }
  state ??= { addresses, setup: {}, createdAt: new Date().toISOString() };
  assert.deepEqual(state.addresses, addresses);
  if (mode === '--setup') {
    // Deploy before acquiring Energy, so the later user's consumption has a clean baseline.
    await setupStep('deploy', options => tron.transactionBuilder.createSmartContract({ ...options, abi: ABI,
      bytecode: INIT_CODE, name: 'NileEnergyProbe', feeLimit: FEE_LIMIT, callValue: 0,
      userFeePercentage: 100, originEnergyLimit: 1 }, addresses.sponsor));
    state.contractAddress = tron.address.fromHex(state.setup.deploy.info.contract_address); await save();
    await setupStep('stakeEnergy', options => tron.transactionBuilder.freezeBalanceV2(STAKE_SUN, 'ENERGY', addresses.sponsor, options));
    await setupStep('delegateEnergy', options => tron.transactionBuilder.delegateResource(STAKE_SUN, addresses.user, 'ENERGY', addresses.sponsor, false, undefined, options));
  } else if (mode === '--prepare') {
    assert(!state.order && state.setup.delegateEnergy?.status === 'solidified');
    const c = await query('getcontract', { value: state.contractAddress, visible: true });
    const code = await query('getcontractinfo', { value: state.contractAddress, visible: true });
    assert.equal(code.runtimecode, RUNTIME); assert.equal(c.consume_user_resource_percent, 100);
    assert.equal(hex(c.origin_address), hex(addresses.sponsor));
    const simulation = await query('triggerconstantcontract', { owner_address: hex(addresses.user), contract_address: hex(state.contractAddress), data: CALL_DATA });
    assert.equal(simulation.result?.result, true);
    assert(!simulation.transaction?.ret?.some(x => x.ret === 'FAILED' || (x.contractRet && x.contractRet !== 'SUCCESS')));
    assert.deepEqual(simulation.constant_result, ['1'.padStart(64, '0')]);
    assert(simulation.energy_used > 0 && simulation.energy_used < 50);
    checkResources(await snapshot(), simulation.energy_used, 0);
    const options = headerOptions(await query('getnowblock'));
    options.blockHeader.expiration = options.blockHeader.timestamp + 1_800_000;
    const { transaction } = await tron.transactionBuilder.triggerSmartContract(state.contractAddress, 'probe()', { ...options, txLocal: true, feeLimit: FEE_LIMIT }, [], addresses.user);
    checkCall(transaction, addresses, state.contractAddress);
    const params = await query('getchainparameters');
    const feePerByte = params.chainParameter.find(p => p.key === 'getTransactionFee').value;
    assert.equal(feePerByte, 1000);
    const pb = utils.transaction.txJsonToPb(transaction);
    pb.addSignature(Buffer.alloc(65)); // Encoding size only, never a valid wallet signature.
    const bandwidth = pb.serializeBinary().length + RESULT_BANDWIDTH;
    assert(bandwidth < 300);
    state.order = { unsigned: transaction, energy: simulation.energy_used, simulation, contractSettings: c,
      bandwidth, feePerByte, bandwidthFeeSun: bandwidth * feePerByte }; await save();
  } else if (mode === '--fund-bandwidth') {
    assert(state.order && !state.call);
    if (!state.setup.fundBandwidth) {
      const before = await snapshot(); assert.equal(before.user.account.balance ?? 0, 0);
      assert(Date.now() < state.order.unsigned.raw_data.expiration);
      const r = before.user.resources;
      assert((r.NetLimit ?? 0) - (r.NetUsed ?? 0) < state.order.bandwidth);
      assert((r.freeNetLimit ?? 0) - (r.freeNetUsed ?? 0) < state.order.bandwidth);
    }
    await setupStep('fundBandwidth', options => tron.transactionBuilder.sendTrx(addresses.user, state.order.bandwidthFeeSun, addresses.sponsor, options));
  } else if (mode === '--submit') {
    if (!state.call) {
      const signed = JSON.parse(await readFile(root + 'tron-energy-wallet-signed.json', 'utf8'));
      const signature = verifySignature(state, signed, Date.now());
      assert.equal(state.setup.fundBandwidth?.status, 'solidified');
      const before = await snapshot(); checkResources(before, state.order.energy, state.order.bandwidthFeeSun);
      state.call = { signed, signature, before, status: 'signed' }; await save();
    }
    if (state.call.status !== 'solidified') checkResources(await snapshot(), state.order.energy, state.order.bandwidthFeeSun);
    await submit(state.call); await audit();
  } else await audit();
  console.log(JSON.stringify({ mode, contractAddress: state.contractAddress, call: state.call?.checks, energy: state.order?.energy }));
} finally { await lock.close(); await unlink(path + '.lock'); }
