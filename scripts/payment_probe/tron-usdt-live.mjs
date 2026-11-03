// Isolated official Nile USDT transfer. No private keys; signatures come from TronLink.
// This baseline measures deployer sponsorship and never claims platform Energy sponsorship.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { open, readFile, writeFile, rename, unlink } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { post, rpc } from './rpc.mjs';
import { ENDPOINT, CHAIN_ID, hex } from './tron-live-core.mjs';
import { USDT, AMOUNT_UNITS, wordAddress, transferData, buildTransfer, checkTransfer, verifySignature,
  simulationEnergy, energyAttribution, checkTransferEvent } from './tron-usdt-core.mjs';

const mode = process.argv[2];
assert(process.argv.length === 3 && ['--prepare', '--submit', '--audit', '--replay'].includes(mode));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const path = `${root}/tron-usdt-state.json`;
const addresses = JSON.parse(await readFile(`${root}/tron-live-addresses.json`, 'utf8'));
assert.deepEqual(addresses, { user: 'TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio', sponsor: 'TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH' });
const ORIGIN = 'TKGRE6oiU3rEzasue4MsB6sCXXSTx9BAe3';
let state;
const save = async () => { await writeFile(`${path}.tmp`, JSON.stringify(state, null, 2) + '\n', { mode: 0o600 }); await rename(`${path}.tmp`, path); };
async function query(method, body = {}, solid = false) {
  const value = await post(`${ENDPOINT}/${solid ? 'walletsolidity' : 'wallet'}/${method}`, body, { direct: true });
  assert(!value.Error, `RPC ${method}: ${value.Error}`); return value;
}
async function tokenBalance(address) {
  const r = await query('triggerconstantcontract', { owner_address: hex(addresses.user), contract_address: hex(USDT),
    function_selector: 'balanceOf(address)', parameter: wordAddress(address) });
  assert.equal(r.result?.result, true); assert.match(r.constant_result?.[0] ?? '', /^[0-9a-fA-F]{64}$/);
  return BigInt(`0x${r.constant_result[0]}`).toString();
}
async function snapshot() {
  const [user, sponsor, contract, originResources] = await Promise.all([
    account(addresses.user), account(addresses.sponsor), query('getcontract', { value: USDT, visible: true }),
    query('getaccountresource', { address: ORIGIN, visible: true }),
  ]);
  assert.equal(contract.origin_address, ORIGIN); assert.equal(contract.contract_address, USDT);
  return { observedAt: new Date().toISOString(), user, sponsor, originResources,
    contract: { address: USDT, origin: ORIGIN, userPercent: contract.consume_user_resource_percent ?? 0, originEnergyLimit: contract.origin_energy_limit } };
}
async function account(address) {
  const [value, resources, tokenUnits] = await Promise.all([query('getaccount', { address, visible: true }),
    query('getaccountresource', { address, visible: true }), tokenBalance(address)]);
  assert(value.address, 'Test account not activated');
  return { trxSun: value.balance ?? 0, tokenUnits, resources };
}
async function simulate() {
  const raw = await query('triggerconstantcontract', { owner_address: hex(addresses.user), contract_address: hex(USDT), data: transferData(addresses.sponsor) });
  return { raw, energy: simulationEnergy(raw, addresses) };
}
function requireResources(before, bandwidth, energy) {
  assert.equal(before.user.trxSun, 0, 'This probe requires a zero-TRX user');
  assert(BigInt(before.user.tokenUnits) >= AMOUNT_UNITS);
  assert.equal(before.contract.userPercent, 0, 'Review changed deployer sponsorship configuration');
  const availableEnergy = (before.originResources.EnergyLimit ?? 0) - (before.originResources.EnergyUsed ?? 0);
  assert(Math.min(availableEnergy, before.contract.originEnergyLimit) >= energy + 2000, 'Deployer Energy cannot cover this baseline');
  assert.equal(before.user.resources.EnergyLimit ?? 0, 0, 'Review caller Energy attribution');
  const staked = (before.user.resources.NetLimit ?? 0) - (before.user.resources.NetUsed ?? 0);
  const free = (before.user.resources.freeNetLimit ?? 0) - (before.user.resources.freeNetUsed ?? 0);
  assert(staked < bandwidth && free >= bandwidth + 32, 'This baseline expects free caller bandwidth, not platform delegation');
  return { energyEstimate: energy, bandwidthEstimate: bandwidth, availableOriginEnergy: availableEnergy,
    availableFreeBandwidth: free, availableStakedBandwidth: staked, expectedEnergySource: 'token_deployer', expectedBandwidthSource: 'user_free_quota' };
}
async function confirmation(signed) {
  const info = await query('gettransactioninfobyid', { value: signed.txID }, true);
  if (!info.id) return null;
  const transaction = await query('gettransactionbyid', { value: signed.txID }, true);
  assert.equal(info.id, signed.txID); assert.equal(transaction.raw_data_hex, signed.raw_data_hex);
  return { info, transaction, status: transaction.ret?.every(value => value.contractRet === 'SUCCESS') && info.result !== 'FAILED' ? 'solidified' : 'failed' };
}
async function waitConfirmation() {
  for (const start = Date.now(); Date.now() - start < 110000;) {
    const value = await confirmation(state.payment.signed); if (value) return value;
    await new Promise(resolve => setTimeout(resolve, 3000));
  }
  throw new Error('No solidity confirmation yet; inspect saved txID before retrying');
}
async function audit() {
  assert(state.payment, 'No payment to audit');
  const confirmed = await confirmation(state.payment.signed); assert(confirmed, 'Not yet solidified');
  Object.assign(state.payment, confirmed); await save();
  assert.equal(state.payment.status, 'solidified', 'Transaction failed; evidence retained');
  const { info, transaction, signed, before } = state.payment;
  checkTransfer(transaction, addresses);
  assert.deepEqual(transaction.signature.map(s => Buffer.from(s, 'hex')), signed.signature.map(s => Buffer.from(s, 'hex')));
  checkTransferEvent(info, addresses);
  const energy = energyAttribution(info.receipt);
  assert(energy.originPaidAll, 'Energy attribution differs from expected deployer payment');
  assert.equal(info.fee ?? 0, 0); assert.equal(info.receipt.net_fee ?? 0, 0);
  const after = state.payment.after ?? await snapshot();
  assert.equal(BigInt(before.user.tokenUnits) - BigInt(after.user.tokenUnits), AMOUNT_UNITS);
  assert.equal(BigInt(after.sponsor.tokenUnits) - BigInt(before.sponsor.tokenUnits), AMOUNT_UNITS);
  for (const role of ['user', 'sponsor']) assert.equal(after[role].trxSun, before[role].trxSun);
  const freeUsed = (after.user.resources.freeNetUsed ?? 0) - (before.user.resources.freeNetUsed ?? 0);
  assert(info.receipt.net_usage > 0 && freeUsed > 0 && Math.abs(info.receipt.net_usage - freeUsed) <= 2);
  assert((after.user.resources.NetUsed ?? 0) <= (before.user.resources.NetUsed ?? 0), 'Unexpected delegated bandwidth consumption');
  state.payment.after = after;
  state.payment.checks = { tokenTransferVerified: true, userTrxUnchanged: true, sponsorTrxUnchanged: true,
    energy, bandwidthUsed: info.receipt.net_usage, bandwidthSource: 'user_free_quota', platformEnergySponsorshipProven: false };
  await save();
  await writeFile(`${root}/tron-usdt-public-result.json`, JSON.stringify({ observedAt: new Date().toISOString(), network: 'Nile', token: USDT,
    walletVersion: '4.11.2', addresses, orderReference: state.order.reference, amountUnits: AMOUNT_UNITS.toString(),
    payment: state.payment, replay: state.replay }, null, 2) + '\n');
}
const lock = await open(`${path}.lock`, 'wx', 0o600);
try {
  assert.equal(await rpc(ENDPOINT + '/jsonrpc', 'eth_chainId', [], { direct: true }), CHAIN_ID);
  try { state = JSON.parse(await readFile(path, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  state ??= { createdAt: new Date().toISOString(), addresses, token: USDT };
  assert.deepEqual(state.addresses, addresses); assert.equal(state.token, USDT);
  if (mode === '--prepare') {
    assert(!state.order, 'Do not replace existing order');
    const [before, simulation, block] = await Promise.all([snapshot(), simulate(), query('getnowblock')]);
    assert(BigInt(before.user.tokenUnits) >= AMOUNT_UNITS);
    const unsigned = await buildTransfer(addresses, block); checkTransfer(unsigned, addresses);
    state.order = { reference: randomUUID(), addresses, unsigned, before, simulation }; await save();
  } else if (mode === '--submit') {
    assert(state.order);
    if (!state.payment) {
      const signed = JSON.parse(await readFile(`${root}/tron-usdt-wallet-signed.json`, 'utf8'));
      const signature = verifySignature(state.order, signed, Date.now());
      const [before, simulation] = await Promise.all([snapshot(), simulate()]);
      const resources = requireResources(before, signature.bandwidthEstimate, simulation.energy);
      state.payment = { txID: signed.txID, signed, before, simulation, signature, resources, status: 'signed' }; await save();
    }
    if (state.payment.status === 'signed') {
      const existing = await query('gettransactionbyid', { value: state.payment.txID });
      if (!existing.txID) {
        assert(Date.now() < state.payment.signed.raw_data.expiration, 'Signature expired; no automatic replacement');
        requireResources(await snapshot(), state.payment.signature.bandwidthEstimate, state.payment.simulation.energy);
        state.payment.broadcast = await query('broadcasttransaction', state.payment.signed); await save();
        assert(state.payment.broadcast.result === true || state.payment.broadcast.code === 'DUP_TRANSACTION_ERROR', JSON.stringify(state.payment.broadcast));
      }
      state.payment.status = 'submitted'; await save();
    }
    Object.assign(state.payment, await waitConfirmation()); await save(); await audit();
  } else if (mode === '--replay') {
    assert.equal(state.payment?.status, 'solidified');
    const before = await snapshot();
    const response = await query('broadcasttransaction', state.payment.signed);
    assert(response.result === undefined || response.result === false); assert.equal(response.code, 'DUP_TRANSACTION_ERROR');
    const after = await snapshot();
    for (const role of ['user', 'sponsor']) {
      assert.equal(after[role].tokenUnits, before[role].tokenUnits); assert.equal(after[role].trxSun, before[role].trxSun);
    }
    state.replay = { observedAt: new Date().toISOString(), response, balancesUnchanged: true }; await save(); await audit();
  } else await audit();
  console.log(JSON.stringify({ mode, txID: state.order?.unsigned.txID, status: state.payment?.status, checks: state.payment?.checks }));
} finally { await lock.close(); await unlink(`${path}.lock`); }
