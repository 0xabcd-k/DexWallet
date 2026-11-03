import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { beginCell } from '@ton/ton';

// Offline reconciliation of archived real-wallet/testnet evidence; never signs or broadcasts.
const directory = resolve(process.argv[2] ?? 'docs/design/工程工作记录/05-钱包与链上通道/5.1-用户钱包接入/attachments/ton-jetton-relay-2026-11-03');
const read = async name => JSON.parse(await readFile(resolve(directory, name), 'utf8'));
const hex = base64 => Buffer.from(base64, 'base64').toString('hex');
const [proof, masters, head, replay, decoded] = await Promise.all([
  'source-proof.json', 'master-reference.json', 'confirmation-head.json', 'payment-replay.json', 'payment-chain-report.json',
].map(read));
assert.equal(masters.testnet.code_hash, proof.compiledMasterHash);
assert.equal(masters.mainnetReadOnly.code_hash, proof.compiledMasterHash);
assert.equal(masters.testnet.jetton_wallet_code_hash, proof.compiledWalletHash);
const reference = beginCell().storeUint(2, 8).storeBuffer(Buffer.from(proof.compiledWalletHash, 'base64')).endCell({ exotic: true });
assert.equal(reference.hash().toString('base64'), masters.mainnetReadOnly.jetton_wallet_code_hash);
assert.equal(proof.mainnetTransactionsSent, false);
const results = {};
for (const mode of ['claim', 'payment']) {
  const [trace, relay, preflight] = await Promise.all([`${mode}-trace.json`, `${mode}-relay.json`, `${mode}-preflight.json`].map(read));
  const txs = Object.values(trace.transactions), before = relay.before.accounts, after = relay.after.accounts;
  assert.equal(hex(trace.external_hash), relay.externalHash);
  assert.equal(trace.is_incomplete, false);
  assert.equal(trace.trace_info.trace_state, 'complete'); assert.equal(trace.trace_info.pending_messages, 0);
  assert.equal(txs.length, mode === 'claim' ? 7 : 6);
  assert.equal(trace.trace_info.transactions, txs.length);
  assert(head.result.last.seqno > Number(trace.mc_seqno_end));
  assert.equal(preflight.signatureVerified, true); assert.equal(preflight.bodyHash, relay.authorization.bodyHash);
  assert.equal(preflight.rejected.length, mode === 'claim' ? 7 : 12);
  let networkCost = 0n, links = 0, skippedNotifications = 0;
  for (const tx of txs) {
    assert.equal(tx.emulated, false); assert.equal(tx.finality, 'finalized');
    assert.equal(tx.mc_block_seqno, Number(trace.mc_seqno_end));
    assert(tx.now < relay.externalValidUntil && tx.now < relay.authorization.validUntil);
    const d = tx.description;
    if (tx.incoming.opcode === '0x7362d09c') {
      // One-nanoton non-bounce notification is credited but cannot run W5 code.
      assert.equal(tx.incoming.value, '1'); assert.equal(tx.total_fees, '0');
      assert.equal(d.compute_ph.skipped, true); assert.equal(d.compute_ph.reason, 'no_gas');
      assert.equal(d.credit_ph.credit, '1'); assert.equal(d.aborted, true);
      assert.equal(tx.outgoing.length, 0); skippedNotifications++;
    } else {
      assert.equal(d.aborted, false); assert.equal(d.compute_ph.success, true); assert.equal(d.compute_ph.exit_code, 0);
      assert.equal(d.action.success, true); assert.equal(d.action.result_code, 0); assert.equal(d.action.skipped_actions, 0);
    }
    networkCost += BigInt(tx.total_fees);
    for (const out of tx.outgoing) {
      assert.equal(out.bounced, false);
      const next = txs.filter(t => t.incoming.hash === out.hash); assert.equal(next.length, 1);
      assert.deepEqual(out, next[0].incoming); networkCost += BigInt(out.fwd_fee); links++;
    }
    if (mode === 'payment') {
      const v2 = Object.values(decoded.transactions).flat().find(t => t.hash === hex(tx.hash)); assert(v2);
      assert.equal(v2.totalFees, tx.total_fees);
      assert.equal(v2.incoming.bodyHash, hex(tx.incoming.bodyHash));
    }
  }
  assert.equal(skippedNotifications, 1); assert.equal(links, txs.length - 1);
  const root = trace.transactions[trace.trace_id]; assert.equal(root.incoming.source, null);
  assert.equal(hex(root.incoming.hash), relay.externalHash);
  assert.equal(hex(root.incoming.bodyHash), relay.platformBodyHash);
  const userExecution = txs.find(t => t.incoming.opcode === '0x73696e74'); assert(userExecution);
  assert.equal(hex(userExecution.incoming.bodyHash), relay.authorization.bodyHash);
  for (const role of ['platform', 'user']) assert.equal(after[role].seqno, before[role].seqno + 1);
  const balances = new Map();
  for (const [role, a] of Object.entries(before)) {
    balances.set(a.address, { before: BigInt(a.balance), after: BigInt(after[role].balance) });
    balances.set(a.token.address, { before: BigInt(a.token.tonBalance), after: BigInt(after[role].token.tonBalance) });
  }
  // Claim also touches public giver/master accounts; obtain their balances from the trace.
  for (const t of txs) if (!balances.has(t.account.toLowerCase())) {
    assert(t.account_state_before.balance !== null && t.account_state_after.balance !== null);
    balances.set(t.account.toLowerCase(), { before: BigInt(t.account_state_before.balance), after: BigInt(t.account_state_after.balance) });
  }
  let totalLoss = 0n;
  for (const [address, b] of balances) {
    const own = txs.filter(t => t.account.toLowerCase() === address);
    const delta = own.reduce((sum, t) => sum + BigInt(t.incoming.value ?? 0) - BigInt(t.total_fees)
      - t.outgoing.reduce((s, m) => s + BigInt(m.value) + BigInt(m.fwd_fee), 0n), 0n);
    assert.equal(b.after - b.before, delta, `TON balance mismatch for ${address}`); totalLoss += b.before - b.after;
  }
  assert.equal(totalLoss, networkCost);
  const platformNetSpend = BigInt(before.platform.balance) - BigInt(after.platform.balance);
  const userTonIncrease = BigInt(after.user.balance) - BigInt(before.user.balance);
  if (mode === 'claim') {
    assert.equal(after.user.token.balance, '10000000'); assert.equal(after.platform.token.balance, '0');
  } else {
    assert.equal(before.user.token.balance, '10000000'); assert.equal(after.user.token.balance, '9000000');
    assert.equal(before.platform.token.state, 'uninitialized'); assert.equal(after.platform.token.state, 'active');
    assert.equal(after.platform.token.balance, '1000000');
    const transfer = decoded.transactions.userToken[0].incoming;
    assert.equal(transfer.opcode, '0x0f8a7ea5'); assert.equal(transfer.queryId, relay.expected.queryId);
    assert.equal(transfer.token.amount, '1000000'); assert.equal(transfer.token.owner, before.platform.address);
    assert.equal(transfer.token.responseDestination, before.platform.address); assert.equal(transfer.token.forwardTon, '1');
    const received = decoded.transactions.platformToken[0].incoming;
    assert.equal(received.opcode, '0x178d4519'); assert.equal(received.token.amount, '1000000');
    assert.equal(received.token.owner, before.user.address); assert.equal(received.token.responseDestination, before.platform.address);
    assert.equal(received.queryId, relay.expected.queryId);
    assert.equal(replay.externalHash, relay.externalHash); assert.equal(replay.exitCode, '133');
    assert(Date.parse(replay.observedAt) / 1000 < replay.externalValidUntil);
    assert.deepEqual(replay.before.accounts, replay.after.accounts);
    for (const role of ['platform', 'user']) {
      assert.equal(replay.before.accounts[role].seqno, after[role].seqno);
      assert.equal(replay.before.accounts[role].balance, after[role].balance);
      assert.equal(replay.before.accounts[`${role}Token`].tokenBalance, after[role].token.balance);
      assert.equal(replay.before.accounts[`${role}Token`].balance, after[role].token.tonBalance);
    }
    const excessTx = txs.find(t => t.incoming.opcode === '0xd53276db');
    assert.equal(excessTx.account.toLowerCase(), before.platform.address);
    const reserveIncrease = BigInt(after.platform.token.tonBalance) - BigInt(before.platform.token.tonBalance);
    const oldReserveSpent = BigInt(before.user.token.tonBalance) - BigInt(after.user.token.tonBalance);
    assert.equal(platformNetSpend, networkCost + userTonIncrease + reserveIncrease - oldReserveSpent);
    results.paymentAccounting = { excessReturnedToPlatform: excessTx.incoming.value,
      excessReceiptFee: excessTx.total_fees, newRecipientTokenReserve: reserveIncrease, oldSenderTokenReserveSpent: oldReserveSpent };
  }
  results[mode] = { transactions: txs.length, links, skippedNotifications, finalized: true, masterchainBlock: trace.mc_seqno_end,
    rootTransactionHash: hex(trace.trace_id), networkCost, platformNetSpend, userTonIncrease };
}
console.log(JSON.stringify({ status: 'verified-from-public-evidence', unit: 'nanoton', ...results }, (_k, v) => typeof v === 'bigint' ? v.toString() : v, 2));
