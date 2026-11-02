import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { json } from './base-live-core.mjs';

const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const base = 'http://127.0.0.1:18793';
const response = await fetch(base + '/state');
let state = await response.json();
assert(response.ok, json(state));
assert.equal(state?.status, 'finalized', 'Wait for finality before exporting successful payment evidence');
const audit = await fetch(base + '/audit', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reference: state.reference }) });
state = await audit.json();
assert(audit.ok, json(state));
assert.equal(state.status, 'finalized');
assert(state.audit.usedAuthorizationRejected && state.receiptCheck.userZeroEthPreserved && state.receiptCheck.sponsorFeeVerified);
await writeFile(`${root}/base-live-public-result.json`, json(state) + '\n');
console.log(json({ status: state.status, txHash: state.txHash, receiptCheck: state.receiptCheck, audit: state.audit }));
