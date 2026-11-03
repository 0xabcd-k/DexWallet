import assert from 'node:assert/strict';
import { test } from 'node:test';
import { TronWeb } from 'tronweb';
import { AMOUNT_SUN, DELEGATE_SUN, tron, verifyWalletSignature, requireDelegatedBandwidth, verifyPayment } from './tron-live-core.mjs';

// Deterministic unfunded fixture keys; these are not actual wallet evidence.
const userKey = '1'.padStart(64, '0'), sponsorKey = '2'.padStart(64, '0');
const addresses = { user: TronWeb.address.fromPrivateKey(userKey), sponsor: TronWeb.address.fromPrivateKey(sponsorKey) };
const timestamp = 1_700_000_000_000;
async function fixture() {
  const unsigned = await tron.transactionBuilder.sendTrx(addresses.sponsor, AMOUNT_SUN, addresses.user, {
    blockHeader: { ref_block_bytes: '0001', ref_block_hash: '01'.repeat(8), timestamp, expiration: timestamp + 600_000 },
  });
  const order = { addresses, unsigned: structuredClone(unsigned) };
  const signed = await tron.trx.sign(unsigned, userKey);
  return { order, signed };
}
function resources() {
  return { addresses, user: { account: { address: addresses.user, balance: AMOUNT_SUN, frozenV2: [{}],
    acquired_delegated_frozenV2_balance_for_bandwidth: DELEGATE_SUN }, resources: { NetLimit: 380, freeNetLimit: 600 } },
  sponsor: { account: { balance: 100_000_000 } },
  delegation: { delegatedResource: [{ from: addresses.sponsor, to: addresses.user, frozen_balance_for_bandwidth: DELEGATE_SUN }] } };
}
test('verifies exact user-signed transfer and requires platform resources', async () => {
  const { order, signed } = await fixture();
  const result = verifyWalletSignature(order, signed, timestamp + 1);
  assert(result.signatureVerified && result.bandwidthEstimate > 200);
  assert(requireDelegatedBandwidth(resources(), result.bandwidthEstimate).available > result.bandwidthEstimate);
});
for (const [name, change] of [
  ['amount', tx => { tx.raw_data.contract[0].parameter.value.amount++; }],
  ['recipient', tx => { tx.raw_data.contract[0].parameter.value.to_address = TronWeb.address.toHex(addresses.user); }],
  ['raw bytes', tx => { tx.raw_data_hex = '00' + tx.raw_data_hex.slice(2); }],
  ['txID', tx => { tx.txID = '00'.repeat(32); }],
  ['extra contract', tx => { tx.raw_data.contract.push(structuredClone(tx.raw_data.contract[0])); }],
  ['permission', tx => { tx.raw_data.contract[0].Permission_id = 2; }],
]) test(`rejects changed ${name}`, async () => {
  const { order, signed } = await fixture(); change(signed);
  assert.throws(() => verifyWalletSignature(order, signed, timestamp + 1));
});
test('rejects wrong signer, expired transaction and re-signed different block reference', async () => {
  const { order, signed } = await fixture();
  const wrongSigner = await tron.trx.sign(structuredClone(order.unsigned), sponsorKey, true, true);
  assert.throws(() => verifyWalletSignature(order, wrongSigner, timestamp + 1), /signer/);
  assert.throws(() => verifyWalletSignature(order, signed, timestamp - 1), /validity/);
  assert.throws(() => verifyWalletSignature(order, signed, signed.raw_data.expiration), /validity/);
  const different = await tron.transactionBuilder.sendTrx(addresses.sponsor, AMOUNT_SUN, addresses.user, {
    blockHeader: { ref_block_bytes: '0002', ref_block_hash: '02'.repeat(8), timestamp, expiration: timestamp + 600_000 },
  });
  const reSigned = await tron.trx.sign(different, userKey);
  assert.throws(() => verifyWalletSignature(order, reSigned, timestamp + 1), /changed transaction hash/);
});
test('free bandwidth cannot stand in for insufficient platform delegation', () => {
  const snapshot = resources(); snapshot.user.resources.NetUsed = 200;
  snapshot.user.resources.freeNetLimit = 10000;
  assert.throws(() => requireDelegatedBandwidth(snapshot, 266), /Insufficient platform/);
  snapshot.user.resources.NetUsed = 0; snapshot.user.account.frozenV2 = [{ amount: 1 }];
  assert.throws(() => requireDelegatedBandwidth(snapshot, 266), /own stake/);
});
test('receipt proves principal-only movement and delegated bandwidth, rejects fee or free-quota use', async () => {
  const { order, signed } = await fixture();
  const before = resources(), after = resources();
  after.user.account.balance = 0; after.sponsor.account.balance += AMOUNT_SUN; after.user.resources.NetUsed = 266;
  const info = { id: signed.txID, receipt: { net_usage: 266 } };
  const tx = { ...signed, ret: [{ contractRet: 'SUCCESS' }] };
  assert.equal(verifyPayment(order, signed, info, tx, before, after).burnedSun, 0);
  const upperSignature = { ...tx, signature: tx.signature.map(value => value.toUpperCase()) };
  assert.equal(verifyPayment(order, signed, info, upperSignature, before, after).burnedSun, 0);
  const wrongSignature = { ...tx, signature: ['00'.repeat(65)] };
  assert.throws(() => verifyPayment(order, signed, info, wrongSignature, before, after));
  assert.throws(() => verifyPayment(order, signed, { ...info, fee: 1000 }, tx, before, after), /burned/);
  after.user.resources.freeNetUsed = 266;
  assert.throws(() => verifyPayment(order, signed, info, tx, before, after), /Free user bandwidth/);
});
