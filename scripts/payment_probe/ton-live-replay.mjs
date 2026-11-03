import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { Cell, TonClient, loadMessage } from '@ton/ton';
import { directory, snapshot } from './ton-live.mjs';

// Resubmit exactly the already-consumed platform external message. Never builds
// a fresh platform authorization around the old user signature.
const receipt = JSON.parse(await readFile(new URL('relay.json', directory), 'utf8'));
const wire = await readFile(new URL('relay-wire.boc', directory));
const cell = Cell.fromBoc(wire)[0];
assert.equal(cell.hash().toString('hex'), receipt.externalHash);
const s = loadMessage(cell.beginParse()).body.beginParse();
s.skip(64);
const validUntil = s.loadUint(32);
const before = await snapshot();
assert.equal(before.platform.seqno, receipt.before.platform.seqno + 1);
assert.equal(before.user.seqno, receipt.before.user.seqno + 1);
assert(validUntil > Date.now() / 1000 + 15, 'Too late to distinguish seqno rejection from expiry');
await new Promise(resolve => setTimeout(resolve, 3500));
const client = new TonClient({ endpoint: 'https://testnet.toncenter.com/api/v2/jsonRPC', timeout: 20000 });
let rejection;
try { await client.sendFile(wire); }
catch (error) {
  rejection = { name: error.name, message: error.message, httpStatus: error.response?.status, response: error.response?.data };
}
await writeFile(new URL('replay-rpc-private.json', directory), JSON.stringify(rejection ?? { accepted: true }, null, 2), { mode: 0o600 });
const text = JSON.stringify(rejection);
assert(rejection && /external message|seqno|exitcode|exit_code/i.test(text), 'No chain-specific replay rejection; inspect private response');
await new Promise(resolve => setTimeout(resolve, 6000));
const after = await snapshot();
for (const role of ['platform', 'user']) {
  assert.equal(after[role].seqno, before[role].seqno);
  assert.equal(after[role].balance, before[role].balance);
  assert.deepEqual(after[role].lastTransaction, before[role].lastTransaction);
}
const exitCode = text.match(/(?:exitcode|exit_code|exit code)[^\d]{0,6}(\d+)/i)?.[1];
const report = { observedAt: new Date().toISOString(), externalHash: receipt.externalHash,
  externalValidUntil: validUntil, chainRejectedReplay: true, exitCode, before, after,
  balancesSeqnosAndLastTransactionsUnchanged: true };
await writeFile(new URL('replay-report.json', directory), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
console.log(JSON.stringify(report, null, 2));
