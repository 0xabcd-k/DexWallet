// Nile-only resource isolation probe. This is not a token or payment contract.
import assert from 'node:assert/strict';
import { utils } from 'tronweb';
import { tron, hex, RESULT_BANDWIDTH } from './tron-live-core.mjs';

// Hand-assembled TVM: PUSH1 1, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN.
// Returns the ABI word 1, without storage writes, external calls or asset movement.
export const RUNTIME = '600160005260206000f3';
// Copy the ten runtime bytes starting at byte 12 into memory and return them.
export const INIT_CODE = '600a600c600039600a6000f3' + RUNTIME;
export const ABI = [{ name: 'probe', type: 'function', inputs: [], outputs: [{ name: '', type: 'uint256' }], stateMutability: 'nonpayable' }];
export const CALL_DATA = tron.sha3('probe()').slice(2, 10);
export const FEE_LIMIT = 1_000_000;
export const STAKE_SUN = 1_000_000;

export function checkCall(tx, addresses, contractAddress) {
  assert.equal(tx.raw_data.contract.length, 1);
  const c = tx.raw_data.contract[0];
  assert.equal(c.type, 'TriggerSmartContract');
  assert.equal(c.Permission_id ?? 0, 0);
  assert.equal(c.parameter.type_url, 'type.googleapis.com/protocol.TriggerSmartContract');
  assert.deepEqual(c.parameter.value, { owner_address: hex(addresses.user), contract_address: hex(contractAddress), data: CALL_DATA });
  assert.equal(tx.raw_data.fee_limit, FEE_LIMIT);
  assert.equal(tx.raw_data.data, undefined);
  assert(utils.transaction.txCheck(tx));
}
export function verifySignature(state, signed, now) {
  checkCall(signed, state.addresses, state.contractAddress);
  assert.equal(signed.txID, state.order.unsigned.txID);
  assert.equal(signed.raw_data_hex, state.order.unsigned.raw_data_hex);
  assert.deepEqual(signed.raw_data, state.order.unsigned.raw_data);
  assert.equal(signed.signature?.length, 1);
  assert.equal(tron.trx.ecRecover(signed), state.addresses.user);
  assert(now >= signed.raw_data.timestamp && now < signed.raw_data.expiration);
  const pb = utils.transaction.txJsonToPb(signed); pb.addSignature(Buffer.from(signed.signature[0], 'hex'));
  assert.equal(pb.serializeBinary().length + RESULT_BANDWIDTH, state.order.bandwidth);
  return { signatureVerified: true, bandwidthEstimate: state.order.bandwidth };
}
export function checkResources(before, expectedEnergy, bandwidthFeeSun) {
  // Diagnostic-only platform pre-funding for Bandwidth, accounted separately.
  assert.equal(before.user.account.balance ?? 0, bandwidthFeeSun);
  assert(before.user.account.frozenV2?.every(item => !item.amount));
  assert.equal(before.user.account.account_resource?.acquired_delegated_frozenV2_balance_for_energy, STAKE_SUN);
  const grants = before.delegation.delegatedResource;
  assert.equal(grants.length, 1);
  assert.equal(hex(grants[0].from), hex(before.addresses.sponsor));
  assert.equal(hex(grants[0].to), hex(before.addresses.user));
  assert.equal(grants[0].frozen_balance_for_energy, STAKE_SUN);
  const r = before.user.resources;
  assert((r.EnergyLimit ?? 0) - (r.EnergyUsed ?? 0) >= expectedEnergy);
}
export function checkReceipt(info, before, after, expectedEnergy, bandwidthFeeSun) {
  assert.notEqual(info.result, 'FAILED');
  assert.equal(info.receipt.result, 'SUCCESS');
  assert.deepEqual(info.contractResult, ['1'.padStart(64, '0')]);
  assert.equal(info.receipt.energy_usage_total, expectedEnergy);
  assert.equal(info.receipt.energy_usage, expectedEnergy);
  assert.equal(info.receipt.origin_energy_usage ?? 0, 0);
  assert.equal(info.fee, bandwidthFeeSun);
  assert.equal(info.receipt.energy_fee ?? 0, 0);
  assert.equal(info.receipt.net_fee, bandwidthFeeSun);
  assert.equal(after.user.account.balance ?? 0, 0);
  assert.equal(before.user.account.balance, bandwidthFeeSun);
  assert.equal(after.sponsor.account.balance, before.sponsor.account.balance);
  const used = (after.user.resources.EnergyUsed ?? 0) - (before.user.resources.EnergyUsed ?? 0);
  assert(used > 0 && used <= expectedEnergy && expectedEnergy - used <= 1);
  return { platformDelegatedEnergyUsed: expectedEnergy, originEnergyUsed: 0, energyBurnedSun: 0,
    bandwidthBurnedSun: bandwidthFeeSun, bandwidthFunding: 'platform pre-funded test TRX; not delegated bandwidth', assetTransfer: false };
}
