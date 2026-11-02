import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT, verifyReceipt } from './solana-live-core.mjs';

const base = 'http://127.0.0.1:18792';
const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const replayRequested = process.argv.includes('--replay');
assert(process.argv.slice(2).every(arg => arg === '--replay'), 'Only --replay is supported');
async function get() {
  const response = await fetch(`${base}/state`);
  const body = await response.json();
  assert(response.ok, JSON.stringify(body));
  return body;
}
async function post(path, reference) {
  const response = await fetch(base + path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reference }) });
  return { httpStatus: response.status, body: await response.json() };
}
let state = await get();
assert(state?.signature, 'No submitted wallet payment; this checker never creates one');
for (let attempt = 0; attempt < 20 && state.status !== 'finalized'; attempt++) {
  await new Promise(resolve => setTimeout(resolve, 1500));
  state = await get();
}
assert.equal(state.status, 'finalized', 'Finalization pending; inspect the existing transaction');
verifyReceipt(state, state.receipt);
await writeFile(`${output}/solana-live-public-result.json`, JSON.stringify(state, null, 2));
if (replayRequested) {
  async function balances() {
    const { value } = await rpc(DEVNET_ENDPOINT, 'getMultipleAccounts', [Object.values(state.addresses), { commitment: 'confirmed', encoding: 'base64' }]);
    return Object.fromEntries(Object.keys(state.addresses).map((role, index) => [role, value[index].lamports]));
  }
  const before = await balances();
  const repeated = await post('/submit', state.reference);
  assert.equal(repeated.httpStatus, 200);
  assert.equal(repeated.body.reusedSubmission, true);
  assert.equal(repeated.body.signature, state.signature);
  const replay = await post('/replay', state.reference);
  const after = await balances();
  const evidence = {
    observedAt: new Date().toISOString(), reference: state.reference, signature: state.signature, before, after,
    repeatedSubmit: { httpStatus: repeated.httpStatus, reusedSubmission: repeated.body.reusedSubmission, signature: repeated.body.signature },
    replay: { httpStatus: replay.httpStatus, signature: replay.body.replaySignature, error: replay.body.error },
    balancesUnchanged: JSON.stringify(before) === JSON.stringify(after),
  };
  // Preserve failures as failures; an expired replay is not proof of accepted network deduplication.
  await writeFile(`${output}/solana-live-replay.json`, JSON.stringify(evidence, null, 2));
  assert.deepEqual(after, before);
  if (replay.httpStatus === 200) assert.equal(replay.body.replaySignature, state.signature);
  else assert.match(replay.body.error ?? '', /BlockhashNotFound/, 'Unexpected replay rejection');
  console.log(JSON.stringify(evidence, null, 2));
}
console.log(JSON.stringify({ signature: state.signature, status: state.status, receiptCheck: state.receiptCheck }, null, 2));
