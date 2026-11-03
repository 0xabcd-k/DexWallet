import assert from 'node:assert/strict';
import { test } from 'node:test';
import { TronWeb } from 'tronweb';
import { tron, hex } from './tron-live-core.mjs';
import { USDT, AMOUNT_UNITS, TRANSFER_TOPIC, wordAddress, buildTransfer, verifySignature,
  simulationEnergy, energyAttribution, checkTransferEvent } from './tron-usdt-core.mjs';

// Public unfunded fixtures. These signatures are not real-wallet evidence.
const key = '1'.padStart(64, '0'), otherKey = '2'.padStart(64, '0');
const addresses = { user: TronWeb.address.fromPrivateKey(key), sponsor: TronWeb.address.fromPrivateKey(otherKey) };
const timestamp = 1_700_000_000_000;
async function fixture() {
  const unsigned = await buildTransfer(addresses, { blockID: 'ab'.repeat(32), block_header: { raw_data: { timestamp } } });
  const order = { addresses, unsigned: structuredClone(unsigned) };
  return { order, signed: await tron.trx.sign(unsigned, key) };
}
test('accepts exact USDT transfer signature and estimates its serialized bandwidth', async () => {
  const { order, signed } = await fixture();
  const result = verifySignature(order, signed, timestamp + 1);
  assert.equal(result.signatureVerified, true); assert(result.bandwidthEstimate > 266);
});
for (const [name, change] of [
  ['token contract', tx => { tx.raw_data.contract[0].parameter.value.contract_address = hex(addresses.sponsor); }],
  ['recipient', tx => { tx.raw_data.contract[0].parameter.value.data = tx.raw_data.contract[0].parameter.value.data.replace(wordAddress(addresses.sponsor), wordAddress(addresses.user)); }],
  ['amount', tx => { tx.raw_data.contract[0].parameter.value.data = tx.raw_data.contract[0].parameter.value.data.slice(0, -1) + '1'; }],
  ['attached TRX', tx => { tx.raw_data.contract[0].parameter.value.call_value = 1; }],
  ['fee limit', tx => { tx.raw_data.fee_limit++; }],
  ['extra contract', tx => { tx.raw_data.contract.push(structuredClone(tx.raw_data.contract[0])); }],
  ['permission', tx => { tx.raw_data.contract[0].Permission_id = 2; }],
  ['raw bytes', tx => { tx.raw_data_hex = '00' + tx.raw_data_hex.slice(2); }],
]) test(`rejects changed ${name}`, async () => {
  const { order, signed } = await fixture(); change(signed);
  assert.throws(() => verifySignature(order, signed, timestamp + 1));
});
test('rejects wrong signer and expiration', async () => {
  const { order, signed } = await fixture();
  const wrong = await tron.trx.sign(structuredClone(order.unsigned), otherKey, true, true);
  assert.throws(() => verifySignature(order, wrong, timestamp + 1), /signer/);
  assert.throws(() => verifySignature(order, signed, signed.raw_data.expiration), /validity/);
});
test('rejects reverted simulations and accepts known USDT false return only with exact call and event', async () => {
  const { order } = await fixture();
  const event = { address: hex(USDT).slice(2), topics: [TRANSFER_TOPIC, wordAddress(addresses.user), wordAddress(addresses.sponsor)], data: AMOUNT_UNITS.toString(16).padStart(64, '0') };
  const response = { result: { result: true }, transaction: order.unsigned, logs: [event], constant_result: ['0'.repeat(64)], energy_used: 29650 };
  assert.equal(simulationEnergy(response, addresses), 29650);
  assert.throws(() => simulationEnergy({ ...response, transaction: { ...order.unsigned, ret: [{ contractRet: 'REVERT' }] }, energy_used: 10 }, addresses), /reverted/);
  assert.throws(() => simulationEnergy({ ...response, transaction: { ...order.unsigned, ret: [{ ret: 'FAILED' }] }, constant_result: [''], energy_used: 1984 }, addresses), /failed/);
  assert.throws(() => simulationEnergy({ ...response, logs: [] }, addresses), /Transfer event/);
  assert.throws(() => simulationEnergy(response, { ...addresses, sponsor: addresses.user }));
});
test('deployer-paid Energy is recorded as origin consumption, not platform delegation', () => {
  assert.deepEqual(energyAttribution({ energy_usage_total: 64000, origin_energy_usage: 64000 }),
    { total: 64000, caller: 0, origin: 64000, burnedSun: 0, originPaidAll: true });
  assert.equal(energyAttribution({ energy_usage_total: 64000, energy_usage: 10000, origin_energy_usage: 54000 }).originPaidAll, false);
  assert.throws(() => energyAttribution({ energy_usage_total: 64000, origin_energy_usage: 63000 }), /Unaccounted/);
});
test('matches actual token Transfer event; rejects other tokens and wrong amounts', () => {
  const event = { address: hex(USDT).slice(2), topics: [TRANSFER_TOPIC, wordAddress(addresses.user), wordAddress(addresses.sponsor)], data: AMOUNT_UNITS.toString(16).padStart(64, '0') };
  checkTransferEvent({ log: [event] }, addresses);
  assert.throws(() => checkTransferEvent({ log: [{ ...event, address: hex(addresses.user).slice(2) }] }, addresses));
  assert.throws(() => checkTransferEvent({ log: [{ ...event, data: '1'.padStart(64, '0') }] }, addresses));
  assert.throws(() => checkTransferEvent({ log: [] }, addresses));
});
