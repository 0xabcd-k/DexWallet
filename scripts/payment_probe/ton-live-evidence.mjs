import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// Offline reconciliation of this experiment's public evidence. No RPC, keys or signing.
const directory = resolve(process.argv[2] ?? 'docs/design/工程工作记录/05-钱包与链上通道/5.1-用户钱包接入/attachments/ton-native-relay-2026-11-03');
const read = async name => JSON.parse(await readFile(resolve(directory, name), 'utf8'));
const [chain, indexed, relay, preflight, replay, fund, initialized] = await Promise.all([
  'chain-report.json', 'v3-confirmation.json', 'relay.json', 'preflight.json',
  'replay-report.json', 'fund.json', 'after-initialize.json',
].map(read));
const hex = base64 => Buffer.from(base64, 'base64').toString('hex');
const transactions = Object.values(chain.transactions).flat();
const find = hash => { const tx = transactions.find(t => t.hash === hash); assert(tx, `Missing transaction ${hash}`); return tx; };
const hashes = {
  dispatch: '9e065e64ea49ba4819b1c306716d33cc236af7229fb85b8a5d310ae3e0f85696',
  payment: 'a25829c06947349c89015789b193c4a08e2702f538fa144595f265d9f76ffba4',
  receipt: '53d778fb27fe4ed339ba2514b5f3938964dcbc48b6b88a66fdea326cf5e25e98',
};
const { dispatch, payment, receipt } = indexed.transactions;
for (const [name, hash] of Object.entries(hashes)) {
  const tx = indexed.transactions[name], raw = find(hash), d = tx.description;
  assert.equal(hex(tx.hash), hash);
  assert.equal(tx.lt, raw.lt);
  assert.equal(tx.total_fees, raw.totalFees);
  assert.equal(d.aborted, false);
  assert.equal(d.compute_ph.success, true);
  assert.equal(d.compute_ph.exit_code, 0);
  assert.equal(d.action.success, true);
  assert.equal(d.action.result_code, 0);
  assert.equal(d.action.skipped_actions, 0);
  assert.equal(tx.outgoing.length, name === 'receipt' ? 0 : 1);
  assert.equal(tx.mc_block_seqno, 95524260);
  assert.deepEqual(tx.block_ref, dispatch.block_ref);
  assert.equal(tx.trace_id, dispatch.hash);
  assert(tx.now < relay.authorization.validUntil);
}
assert.equal(indexed.head.ok, true);
assert(indexed.head.result.last.seqno > dispatch.mc_block_seqno);
assert.equal(hex(dispatch.incoming.hash), relay.externalHash);
assert.deepEqual(dispatch.outgoing[0], payment.incoming);
assert.deepEqual(payment.outgoing[0], receipt.incoming);
assert.equal(find(hashes.dispatch).incoming.bodyHash, relay.platformBodyHash);
assert.equal(find(hashes.payment).incoming.bodyHash, relay.authorization.bodyHash);
assert.equal(preflight.bodyHash, relay.authorization.bodyHash);
assert.equal(preflight.validUntil, relay.authorization.validUntil);
assert.equal(preflight.validUntil, preflight.requestedValidUntil);
assert.equal(preflight.signatureVerified, true);
assert.equal(preflight.rejectedCases.length, 8);
const platform = relay.before.platform.address, user = relay.before.user.address;
for (const [tx, account] of [[dispatch, platform], [payment, user], [receipt, platform]]) {
  assert.equal(tx.account.toLowerCase(), account);
  assert.equal(tx.incoming.destination.toLowerCase(), account);
}
for (const [message, from, to, value] of [
  [payment.incoming, platform, user, '3000000'], [receipt.incoming, user, platform, '10000000'],
]) {
  assert.equal(message.source.toLowerCase(), from);
  assert.equal(message.destination.toLowerCase(), to);
  assert.equal(message.value, value);
  assert.equal(message.bounced, false);
}
const cost = tx => BigInt(tx.total_fees) + tx.outgoing.reduce((sum, m) => sum + BigInt(m.fwd_fee), 0n);
const dispatchCost = cost(dispatch), executionCost = cost(payment), receiptCost = cost(receipt);
const networkCost = dispatchCost + executionCost + receiptCost;
const principal = BigInt(receipt.incoming.value), attached = BigInt(payment.incoming.value);
const retained = attached - executionCost;
const sponsorship = attached + dispatchCost + receiptCost;
assert.equal(sponsorship, networkCost + retained);
assert.equal(BigInt(relay.before.user.balance) - principal + retained, BigInt(relay.after.user.balance));
assert.equal(BigInt(relay.before.platform.balance) + principal - sponsorship, BigInt(relay.after.platform.balance));
for (const role of ['platform', 'user']) {
  assert.equal(relay.before[role].seqno, 1);
  assert.equal(relay.after[role].seqno, 2);
  assert.deepEqual(initialized[role], relay.before[role]);
  assert.deepEqual(chain.snapshot[role], relay.after[role]);
  assert.deepEqual(replay.before[role], relay.after[role]);
  assert.deepEqual(replay.after[role], replay.before[role]);
}
assert.equal(replay.chainRejectedReplay, true);
assert.equal(replay.exitCode, '133');
assert.equal(replay.externalHash, relay.externalHash);
assert(Date.parse(replay.after.observedAt) / 1000 < replay.externalValidUntil);
assert.equal(hex(relay.after.platform.lastTransaction.hash), hashes.receipt);
assert.equal(hex(relay.after.user.lastTransaction.hash), hashes.payment);

// Bootstrap is a separate wallet-broadcast self-transfer, not the sign-only relay.
const fundTx = find(hex(fund.after.platform.lastTransaction.hash));
const initTx = find('3935ad42a2f01c1c4d6ce9c259feab2120680633435bafe4dd9a3cf0202ac6c8');
const initReceipt = find(hex(initialized.user.lastTransaction.hash));
const rawCost = tx => BigInt(tx.totalFees) + tx.outgoing.reduce((sum, m) => sum + BigInt(m.forwardFee), 0n);
const fundingCost = rawCost(fundTx), initializationCost = rawCost(initTx) + rawCost(initReceipt);
assert.equal(fund.after.user.balance, '50000000');
assert.equal(initTx.incoming.type, 'external-in');
assert.equal(initTx.outgoing[0].value, '1000000');
assert.equal(initReceipt.incoming.value, '1000000');
assert.equal(initTx.outgoing[0].destination, initTx.incoming.destination);
assert.equal(BigInt(fund.before.platform.balance) - 50_000_000n - fundingCost, BigInt(fund.after.platform.balance));
assert.equal(BigInt(fund.after.user.balance) - initializationCost, BigInt(initialized.user.balance));

console.log(JSON.stringify({
  status: 'verified-from-public-evidence', network: 'TON testnet',
  transactionHashes: hashes, masterchainBlock: dispatch.mc_block_seqno,
  observedMasterchainHead: indexed.head.result.last.seqno,
  messageLinksVerified: 2, executionSuccesses: 3, negativeCases: preflight.rejectedCases.length,
  replayRejectedBeforeExpiry: true,
  nanotons: { principal, relayAttached: attached, dispatchCost, executionCost, receiptCost,
    networkCost, surplusRetainedByUser: retained, platformSponsorship: sponsorship,
    userNetDeduction: principal - retained, receiptNetCredit: principal - receiptCost,
    fundingCost, initializationCost },
}, (_key, value) => typeof value === 'bigint' ? value.toString() : value, 2));
