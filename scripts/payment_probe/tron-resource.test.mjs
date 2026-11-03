import assert from 'node:assert/strict';
import test from 'node:test';
import { tron } from './tron-live-core.mjs';
import { USDT, TRANSFER_TOPIC, wordAddress, buildTransfer, verifySignature } from './tron-usdt-core.mjs';
import { resourcePlan, unusedSponsorBandwidthStake, requirePlatformResources, verifyResourcePayment } from './tron-resource-core.mjs';

const user = await tron.createAccount(), sponsor = await tron.createAccount(), fixture = await tron.createAccount();
const addresses = { user: user.address.base58, sponsor: sponsor.address.base58 }, token = fixture.address.base58;
const energy = 14650, bandwidth = 345;
function snapshot() {
  return { observedAt: '2026-11-03T00:00:00Z', completedAt: '2026-11-03T00:00:00Z', addresses, maxDelegatable: { ENERGY: 0, BANDWIDTH: 200000000 }, contract: { consume_user_resource_percent: 100 },
    user: { tokenUnits: '20000000', account: { frozenV2: [{}, { type: 'ENERGY' }],
      acquired_delegated_frozenV2_balance_for_bandwidth: 900000000,
      account_resource: { acquired_delegated_frozenV2_balance_for_energy: 250000000 } },
    resources: { EnergyLimit: 18000, EnergyUsed: 10, NetLimit: 568, NetUsed: 160, freeNetLimit: 600, freeNetUsed: 200,
      TotalEnergyLimit: 180000000000, TotalEnergyWeight: 2450000000, TotalNetLimit: 43200000000, TotalNetWeight: 68500000000 } },
    sponsor: { account: { balance: 10000000 }, tokenUnits: '980000000' },
    delegation: { delegatedResource: [{ from: addresses.sponsor, to: addresses.user, frozen_balance_for_energy: 250000000, frozen_balance_for_bandwidth: 900000000 }] } };
}
function receipt() {
  return { receipt: { result: 'SUCCESS', energy_usage: energy, energy_usage_total: energy, net_usage: bandwidth },
    log: [{ address: tron.address.toHex(token).slice(2).toLowerCase(), topics: [TRANSFER_TOPIC, wordAddress(addresses.user), wordAddress(addresses.sponsor)], data: (10000n).toString(16).padStart(64, '0') }] };
}
function after() {
  const s = snapshot(); s.user.tokenUnits = '19990000'; s.sponsor.tokenUnits = '980010000';
  s.user.resources.EnergyUsed += energy; s.user.resources.NetUsed += bandwidth;
  return s;
}
test('resource sizing uses both live quota ratios and existing delegation; free quota cannot reduce stake', () => {
  const s = snapshot();
  s.user.account.account_resource.acquired_delegated_frozenV2_balance_for_energy = 1000000;
  s.user.account.acquired_delegated_frozenV2_balance_for_bandwidth = 500000000;
  const plan = resourcePlan(s, energy, bandwidth);
  assert(plan.energyShare >= 227000000 && plan.energyShare <= 229000000);
  assert(plan.bandwidthShare >= 852000000 && plan.bandwidthShare <= 854000000);
  assert.equal(plan.stakeEnergy, plan.energyShare - 1000000);
  assert.equal(plan.stakeBandwidth, plan.bandwidthShare - 500000000 - 200000000);
  s.user.resources.freeNetLimit = 1000000;
  assert.deepEqual(resourcePlan(s, energy, bandwidth), plan);
  assert.throws(() => resourcePlan(s, 50000, bandwidth));
});
test('both resource sources must be exclusively attributable to the platform', () => {
  assert.equal(requirePlatformResources(snapshot(), energy, bandwidth).availableBandwidth, 408);
  for (const mutate of [
    s => { s.user.account.balance = 1; },
    s => { s.user.account.frozenV2[0].amount = 1000000; },
    s => { s.user.account.acquired_delegated_frozen_balance_for_bandwidth = 1000000; },
    s => { s.delegation.delegatedResource[0].from = token; },
    s => { s.user.account.account_resource.acquired_delegated_frozenV2_balance_for_energy += 1; },
    s => { s.user.resources.EnergyLimit = energy; },
    s => { s.user.resources.NetLimit = bandwidth; s.user.resources.freeNetLimit = 1000000; },
    s => { s.contract.consume_user_resource_percent = 0; },
  ]) {
    const s = snapshot(); mutate(s); assert.throws(() => requirePlatformResources(s, energy, bandwidth));
  }
});
test('memo setup may delegate only unused own stake, excluding quota already consumed', () => {
  const s = { account: { frozenV2: [{ amount: 480000000 }] }, resources: { NetUsed: 170, TotalNetWeight: 68500000000, TotalNetLimit: 43200000000 } };
  assert.equal(unusedSponsorBandwidthStake(s), 210000000);
  s.account.acquired_delegated_frozenV2_balance_for_bandwidth = 1000000;
  assert.throws(() => unusedSponsorBandwidthStake(s));
});
test('receipt reconciles test token, zero TRX and both delegated resources', () => {
  const result = verifyResourcePayment(receipt(), snapshot(), after(), token, energy, bandwidth);
  assert.equal(result.platformDelegatedEnergyUsed, energy);
  assert.equal(result.platformDelegatedBandwidthUsed, bandwidth);
  const recovered = after(); recovered.user.resources.NetUsed -= 1; recovered.user.resources.freeNetUsed -= 1;
  verifyResourcePayment(receipt(), snapshot(), recovered, token, energy, bandwidth);
});
test('deployer payment, burned TRX, free quota, wrong token and wrong credit never count as success', () => {
  for (const mutate of [
    (r, a) => { r.receipt.origin_energy_usage = 1; },
    (r, a) => { r.receipt.energy_fee = 1; },
    (r, a) => { r.receipt.net_fee = 1; },
    (r, a) => { a.user.resources.freeNetUsed += bandwidth; },
    (r, a) => { r.log[0].address = tron.address.toHex(USDT).slice(2); },
    (r, a) => { a.sponsor.tokenUnits = '980009999'; },
    (r, a) => { a.user.account.balance = 1; },
    (r, a) => { a.user.resources.EnergyUsed -= 10; },
  ]) {
    const r = receipt(), a = after(); mutate(r, a);
    assert.throws(() => verifyResourcePayment(r, snapshot(), a, token, energy, bandwidth));
  }
});
test('slow confirmation permits only time-bounded resource recovery and rejects stale audits', () => {
  const a = after(); a.completedAt = '2026-11-03T00:02:00Z'; a.user.resources.EnergyUsed -= 18;
  verifyResourcePayment(receipt(), snapshot(), a, token, energy, bandwidth);
  a.user.resources.EnergyUsed -= 20;
  assert.throws(() => verifyResourcePayment(receipt(), snapshot(), a, token, energy, bandwidth));
  a.user.account.account_resource.energy_window_size = 14_400_000;
  a.user.account.account_resource.energy_window_optimized = true;
  // Optimized Stake V2 window: 14,400,000 / 1,000 slots * 3 seconds = 12 hours.
  verifyResourcePayment(receipt(), snapshot(), a, token, energy, bandwidth);
  a.completedAt = '2026-11-03T00:16:00Z';
  assert.throws(() => verifyResourcePayment(receipt(), snapshot(), a, token, energy, bandwidth), /interval/);
});
test('user signature for a clone cannot be substituted into the official USDT order', async () => {
  const block = { blockID: 'ab'.repeat(32), block_header: { raw_data: { timestamp: 1000000 } } };
  const unsigned = await buildTransfer(addresses, block, token);
  const signed = await tron.trx.sign(unsigned, user.privateKey);
  const order = { addresses, unsigned };
  assert.equal(verifySignature(order, signed, 1000001, token).signatureVerified, true);
  assert.throws(() => verifySignature(order, signed, 1000001), /differs from saved intent/);
});
