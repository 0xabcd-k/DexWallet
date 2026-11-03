import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT, verifyReceipt } from './solana-live-core.mjs';
import { verifyTokenReceipt } from './solana-token-core.mjs';
import { probeToken } from './solana-token-assets.mjs';

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
const token = state.asset ? probeToken(state.asset) : null;
const probeName = token ? state.probeCase : 'solana-live';
assert(['solana-live', ...(token ? [`${token.probePrefix}-new-ata`, `${token.probePrefix}-existing-ata`] : [])].includes(probeName));
if (token) verifyTokenReceipt(state, state.receipt, token);
else verifyReceipt(state, state.receipt);
await writeFile(`${output}/${probeName}-public-result.json`, JSON.stringify(state, null, 2));
if (replayRequested) {
  async function balances() {
    const entries = Object.entries(state.addresses);
    if (token) entries.push(['userAta', state.tokenAccounts.user], ['recipientAta', state.tokenAccounts.recipient]);
    const { value } = await rpc(DEVNET_ENDPOINT, 'getMultipleAccounts', [entries.map(([, address]) => address), { commitment: 'confirmed', encoding: 'jsonParsed' }]);
    return Object.fromEntries(entries.map(([role], index) => [role, {
      lamports: value[index].lamports, tokenAmount: value[index].data?.parsed?.info?.tokenAmount?.amount,
    }]));
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
  await writeFile(`${output}/${probeName}-replay.json`, JSON.stringify(evidence, null, 2));
  assert.deepEqual(after, before);
  if (replay.httpStatus === 200) assert.equal(replay.body.replaySignature, state.signature);
  else assert.match(replay.body.error ?? '', /BlockhashNotFound|AlreadyProcessed/, 'Unexpected replay rejection');
  console.log(JSON.stringify(evidence, null, 2));
}
console.log(JSON.stringify({ signature: state.signature, status: state.status, receiptCheck: state.receiptCheck }, null, 2));
