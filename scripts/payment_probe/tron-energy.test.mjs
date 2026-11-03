import test from 'node:test';
import assert from 'node:assert/strict';
import { utils } from 'tronweb';
import { tron, headerOptions, hex } from './tron-live-core.mjs';
import { checkCall, checkResources, checkReceipt, verifySignature, FEE_LIMIT, STAKE_SUN } from './tron-energy-core.mjs';
const addresses = { user: tron.address.fromPrivateKey('1'.padStart(64, '0')), sponsor: tron.address.fromPrivateKey('2'.padStart(64, '0')) };
const contract = tron.address.fromPrivateKey('3'.padStart(64, '0'));
const options = headerOptions({ blockID: 'ab'.repeat(32), block_header: { raw_data: { timestamp: 1700000000000 } } });
const snapshot = () => ({ addresses, user: { account: { frozenV2: [], account_resource: { acquired_delegated_frozenV2_balance_for_energy: STAKE_SUN } }, resources: { EnergyLimit: 73, EnergyUsed: 0, freeNetLimit: 600 } }, sponsor: { account: { balance: 12_000_000 } }, delegation: { delegatedResource: [{ from: hex(addresses.sponsor), to: hex(addresses.user), frozen_balance_for_energy: STAKE_SUN }] } });
test('only exact zero-value probe call accepted', async () => {
  const { transaction } = await tron.transactionBuilder.triggerSmartContract(contract, 'probe()', { ...options, txLocal: true, feeLimit: FEE_LIMIT }, [], addresses.user);
  checkCall(transaction, addresses, contract);
  for (const change of [v => { v.call_value = 1; }, v => { v.owner_address = hex(addresses.sponsor); }, v => { v.data = 'deadbeef'; }]) {
    const bad = structuredClone(transaction); change(bad.raw_data.contract[0].parameter.value);
    assert.throws(() => checkCall(bad, addresses, contract));
  }
});
test('Energy quota must be traced to platform delegation, without unexplained TRX or own stake', () => {
  checkResources(snapshot(), 18, 0);
  for (const mutate of [s => { s.user.account.balance = 1; }, s => { s.user.account.frozenV2 = [{ amount: 1 }]; }, s => { s.delegation.delegatedResource[0].from = hex(contract); }, s => { s.user.resources.EnergyUsed = 60; }]) {
    const bad = snapshot(); mutate(bad); assert.throws(() => checkResources(bad, 18, 0));
  }
});
test('wallet signature must match saved intent, user, validity and funded bandwidth size', async () => {
  const { transaction } = await tron.transactionBuilder.triggerSmartContract(contract, 'probe()', { ...options, txLocal: true, feeLimit: FEE_LIMIT }, [], addresses.user);
  const signed = await tron.trx.sign(transaction, '1'.padStart(64, '0'));
  const pb = utils.transaction.txJsonToPb(signed); pb.addSignature(Buffer.from(signed.signature[0], 'hex'));
  const state = { addresses, contractAddress: contract, order: { unsigned: transaction, bandwidth: pb.serializeBinary().length + 64 } };
  assert(verifySignature(state, signed, transaction.raw_data.timestamp).signatureVerified);
  assert.throws(() => verifySignature(state, signed, transaction.raw_data.expiration));
  const wrong = structuredClone(signed);
  wrong.signature = [utils.crypto.ECKeySign(Buffer.from(transaction.txID, 'hex'), Buffer.from('2'.padStart(64, '0'), 'hex'))];
  assert.throws(() => verifySignature(state, wrong, transaction.raw_data.timestamp));
  const changed = structuredClone(state); changed.order.unsigned.txID = '00'.repeat(32);
  assert.throws(() => verifySignature(changed, signed, transaction.raw_data.timestamp));
  state.order.bandwidth--;
  assert.throws(() => verifySignature(state, signed, transaction.raw_data.timestamp));
});
test('deployer Energy and Energy TRX burn cannot count as delegated Energy success', () => {
  const before = snapshot(), after = snapshot(); after.user.resources.EnergyUsed = 18; before.user.account.balance = 280000;
  const info = { fee: 280000, receipt: { result: 'SUCCESS', energy_usage_total: 18, energy_usage: 18, net_fee: 280000 }, contractResult: ['1'.padStart(64, '0')] };
  assert.equal(checkReceipt(info, before, after, 18, 280000).platformDelegatedEnergyUsed, 18);
  for (const mutate of [i => { i.receipt.energy_usage = 0; i.receipt.origin_energy_usage = 18; }, i => { i.receipt.energy_fee = 1; }, i => { i.receipt.result = 'OUT_OF_ENERGY'; }, i => { i.contractResult = ['0'.repeat(64)]; }]) {
    const bad = structuredClone(info); mutate(bad); assert.throws(() => checkReceipt(bad, before, after, 18, 280000));
  }
});
