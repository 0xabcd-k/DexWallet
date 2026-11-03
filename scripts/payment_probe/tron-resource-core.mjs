// Bounded Nile token-clone experiment; never a production asset or fee policy.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { tron, hex, BANDWIDTH_MARGIN } from './tron-live-core.mjs';
import { AMOUNT_UNITS, checkTransferEvent } from './tron-usdt-core.mjs';

export const REFERENCE_CREATION_HASH = '2866816060782939f36b0f42dd695c197b72d4ea133f433a08575c9a528a93b5';
export const REFERENCE_RUNTIME_HASH = '7ae2015d27ec7aec7c057f4a3dd026dfc224a0b9ee4017eccfa569ae167cbb2b';
export const FIXTURE_NAME = 'DexWallet Resource Test';
export const FIXTURE_SYMBOL = 'DWUSDT-PROBE';
export const FIXTURE_SUPPLY = 1_000_000_000n;
export const USER_FUND_UNITS = 20_000_000n;
export const DEPLOY_FEE_LIMIT = 400_000_000;
export const ENERGY_MARGIN = 2_000;
export const SUN_PER_TRX = 1_000_000;
export const RESOURCE_RECOVERY_MS = 24 * 60 * 60 * 1000;
export const MAX_AUDIT_INTERVAL_MS = 15 * 60 * 1000;
const BLOCK_INTERVAL_MS = 3_000;
const WINDOW_SIZE_PRECISION = 1_000;
export const ResourceType = Object.freeze({ BANDWIDTH: 0, ENERGY: 1 });
export const codeHash = code => createHash('sha256').update(Buffer.from(code, 'hex')).digest('hex');

export function fixtureCreation(reference) {
  const { contract, contractInfo } = reference;
  assert.equal(codeHash(contract.bytecode), REFERENCE_CREATION_HASH, 'Reference creation code changed');
  assert.equal(codeHash(contractInfo.runtimecode), REFERENCE_RUNTIME_HASH, 'Reference runtime changed');
  const offset = contract.bytecode.indexOf(contractInfo.runtimecode);
  assert(offset > 0 && offset === contract.bytecode.lastIndexOf(contractInfo.runtimecode));
  const end = offset + contractInfo.runtimecode.length;
  const originalArgs = contract.bytecode.slice(end);
  const [, name, symbol, decimals] = tron.utils.abi.decodeParams([], ['uint256', 'string', 'string', 'uint8'], `0x${originalArgs}`);
  assert.equal(name, 'Tether USD'); assert.equal(symbol, 'USDT'); assert.equal(decimals, 6n);
  const abi = contract.abi.entrys.map(entry => ({ ...entry, type: entry.type.toLowerCase(),
    ...(entry.stateMutability ? { stateMutability: entry.stateMutability.toLowerCase() } : {}) }));
  return { abi, bytecode: contract.bytecode.slice(0, end), originalConstructorBytes: originalArgs.length / 2 };
}

export function resourcePlan(snapshot, energy, bandwidth) {
  assert(Number.isSafeInteger(energy) && energy > 0 && energy <= 40_000);
  assert(Number.isSafeInteger(bandwidth) && bandwidth > 0 && bandwidth <= 400);
  const r = snapshot.user.resources, a = snapshot.user.account;
  const requiredShare = (units, totalLimit, totalWeight) => {
    assert(totalLimit > 0 && totalWeight > 0);
    // Round up to whole staked TRX and keep an extra one for integer quota rounding.
    return (Math.ceil(units * totalWeight / totalLimit) + 1) * SUN_PER_TRX;
  };
  const energyShare = requiredShare(energy + ENERGY_MARGIN + (r.EnergyUsed ?? 0), r.TotalEnergyLimit, r.TotalEnergyWeight);
  const bandwidthShare = requiredShare(bandwidth + BANDWIDTH_MARGIN + (r.NetUsed ?? 0), r.TotalNetLimit, r.TotalNetWeight);
  const additionalEnergy = Math.max(0, energyShare - (a.account_resource?.acquired_delegated_frozenV2_balance_for_energy ?? 0));
  const additionalBandwidth = Math.max(0, bandwidthShare - (a.acquired_delegated_frozenV2_balance_for_bandwidth ?? 0));
  const stakeEnergy = Math.max(0, additionalEnergy - snapshot.maxDelegatable.ENERGY);
  const stakeBandwidth = Math.max(0, additionalBandwidth - snapshot.maxDelegatable.BANDWIDTH);
  return { energy, bandwidth, energyShare, bandwidthShare, additionalEnergy, additionalBandwidth, stakeEnergy, stakeBandwidth };
}

export function unusedSponsorBandwidthStake(sponsor) {
  // getcandelegatedmaxsize additionally reserves a normal delegation's Bandwidth.
  // The isolated setup instead burns TRX for a larger memo transaction; this bound
  // applies only when setup proves that the complete transaction cannot use quota.
  const a = sponsor.account, r = sponsor.resources;
  assert(!a.acquired_delegated_frozenV2_balance_for_bandwidth && !a.acquired_delegated_frozen_balance_for_bandwidth);
  assert(!(a.frozen?.length), 'Unexpected legacy sponsor stake');
  const ownSun = a.frozenV2.find(v => !v.type || v.type === 'BANDWIDTH')?.amount ?? 0;
  const consumedSun = Math.ceil((r.NetUsed ?? 0) * r.TotalNetWeight / r.TotalNetLimit) * SUN_PER_TRX;
  return Math.max(0, ownSun - consumedSun);
}

export function requirePlatformResources(snapshot, energy, bandwidth) {
  const { user, addresses } = snapshot;
  assert.equal(user.account.balance ?? 0, 0, 'Test user must remain zero TRX');
  assert(user.account.frozenV2?.every(item => !item.amount), 'User own stake is not platform funding');
  assert(!(user.account.frozen?.length || user.account.account_resource?.frozen_balance_for_energy?.frozen_balance), 'Unexpected Stake 1.0 resources');
  assert(!(user.account.acquired_delegated_frozen_balance_for_bandwidth || user.account.account_resource?.acquired_delegated_frozen_balance_for_energy), 'Unexpected Stake 1.0 delegation');
  const grants = snapshot.delegation.delegatedResource ?? [];
  assert.equal(grants.length, 1);
  const grant = grants[0];
  assert.equal(hex(grant.from), hex(addresses.sponsor)); assert.equal(hex(grant.to), hex(addresses.user));
  assert(grant.frozen_balance_for_energy > 0 && grant.frozen_balance_for_bandwidth > 0);
  assert.equal(user.account.account_resource?.acquired_delegated_frozenV2_balance_for_energy, grant.frozen_balance_for_energy);
  assert.equal(user.account.acquired_delegated_frozenV2_balance_for_bandwidth, grant.frozen_balance_for_bandwidth);
  const r = user.resources;
  const availableEnergy = (r.EnergyLimit ?? 0) - (r.EnergyUsed ?? 0);
  const availableBandwidth = (r.NetLimit ?? 0) - (r.NetUsed ?? 0);
  assert(availableEnergy >= energy + ENERGY_MARGIN, 'Insufficient delegated Energy');
  assert(availableBandwidth >= bandwidth + BANDWIDTH_MARGIN, 'Insufficient delegated Bandwidth; free quota is not a substitute');
  assert.equal(snapshot.contract.consume_user_resource_percent, 100, 'Deployer sponsorship must be disabled');
  return { availableEnergy, availableBandwidth, energyShareSun: grant.frozen_balance_for_energy, bandwidthShareSun: grant.frozen_balance_for_bandwidth };
}

export function verifyResourcePayment(info, before, after, token, expectedEnergy, expectedBandwidth) {
  assert.notEqual(info.result, 'FAILED'); assert.equal(info.receipt.result, 'SUCCESS');
  checkTransferEvent(info, before.addresses, token);
  assert.equal(info.receipt.energy_usage_total, expectedEnergy);
  assert.equal(info.receipt.energy_usage, expectedEnergy);
  assert.equal(info.receipt.origin_energy_usage ?? 0, 0, 'Deployer paid Energy');
  assert.equal(info.receipt.energy_fee ?? 0, 0); assert.equal(info.receipt.net_fee ?? 0, 0); assert.equal(info.fee ?? 0, 0);
  assert.equal(info.receipt.net_usage, expectedBandwidth);
  for (const role of ['user', 'sponsor']) assert.equal(after[role].account.balance ?? 0, before[role].account.balance ?? 0);
  assert.equal(BigInt(before.user.tokenUnits) - BigInt(after.user.tokenUnits), AMOUNT_UNITS);
  assert.equal(BigInt(after.sponsor.tokenUnits) - BigInt(before.sponsor.tokenUnits), AMOUNT_UNITS);
  // RPC snapshots are not atomic. Stake V2 windows can be shorter than 24 hours.
  // Use the node's stored usage/window at both ends to bound recovery conservatively.
  const elapsedMs = Date.parse(after.completedAt) - Date.parse(before.observedAt);
  assert(Number.isFinite(elapsedMs) && elapsedMs >= 0 && elapsedMs <= MAX_AUDIT_INTERVAL_MS, 'Resource audit interval unavailable or too long');
  const deltas = {}, recoveryBounds = {};
  for (const [key, expected] of [['EnergyUsed', expectedEnergy], ['NetUsed', expectedBandwidth]]) {
    const delta = (after.user.resources[key] ?? 0) - (before.user.resources[key] ?? 0);
    const rate = snapshot => {
      const energy = key === 'EnergyUsed';
      const a = energy ? snapshot.user.account.account_resource ?? {} : snapshot.user.account;
      const encodedWindow = a[energy ? 'energy_window_size' : 'net_window_size'] ?? 0;
      const optimized = a[energy ? 'energy_window_optimized' : 'net_window_optimized'];
      const slots = optimized ? Math.floor(encodedWindow / WINDOW_SIZE_PRECISION) : encodedWindow;
      const windowMs = slots > 0 ? slots * BLOCK_INTERVAL_MS : RESOURCE_RECOVERY_MS;
      const storedUsage = a[energy ? 'energy_usage' : 'net_usage'] ?? snapshot.user.resources[key] ?? 0;
      return storedUsage / windowMs;
    };
    const recoveryBound = Math.ceil((rate(before) + rate(after)) * elapsedMs) + 2;
    assert(delta > 0 && delta <= expected && expected - delta <= recoveryBound, `${key} does not reconcile`);
    deltas[key] = delta;
    recoveryBounds[key] = recoveryBound;
  }
  assert((after.user.resources.freeNetUsed ?? 0) <= (before.user.resources.freeNetUsed ?? 0), 'User free Bandwidth was consumed');
  return { platformDelegatedEnergyUsed: expectedEnergy, platformDelegatedBandwidthUsed: expectedBandwidth,
    userTrxSpentSun: 0, energyFromDeployer: 0, trxBurnedSun: 0, resourceDeltas: deltas,
    resourceRecoveryBounds: recoveryBounds, auditIntervalMs: elapsedMs, testTokenOnly: true };
}
