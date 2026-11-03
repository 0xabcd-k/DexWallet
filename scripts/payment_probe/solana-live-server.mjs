import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, writeFile, rename } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import bs58 from 'bs58';
import { Keypair } from '@solana/web3.js';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT, MAX_FEE_LAMPORTS, digest, prepareOrder, validateUserTransaction, verifyReceipt } from './solana-live-core.mjs';
import { inspectTokenAccounts, prepareTokenOrder, verifyTokenReceipt } from './solana-token-core.mjs';
import { USDC, USDT_FIXTURE } from './solana-token-assets.mjs';

const usdc = process.argv.includes('--usdc');
const fixture = process.argv.includes('--usdt-fixture');
assert(!(usdc && fixture), 'Choose one token probe');
const token = usdc ? USDC : fixture ? USDT_FIXTURE : null;
const existingAta = process.argv.includes('--existing-ata');
assert(process.argv.slice(2).every(arg => ['--usdc', '--usdt-fixture', '--existing-ata'].includes(arg)), 'Unknown probe option');
assert(!existingAta || token, '--existing-ata requires a token probe');
const probeName = token ? `${token.probePrefix}-${existingAta ? 'existing' : 'new'}-ata` : 'solana-live';
const checkReceipt = token ? (order, receipt) => verifyTokenReceipt(order, receipt, token) : verifyReceipt;
const paymentDescription = token
  ? `Dedicated test accounts only. Payment: 0.01 ${token.description}. Platform pays network fees and recipient ATA creation deposit. Case: ${existingAta ? 'existing ATA' : 'new ATA'}.`
  : 'Dedicated test accounts only. Payment: 0.000001 SOL (1000 lamports). Platform pays network fees.';

const here = dirname(fileURLToPath(import.meta.url));
const output = resolve(here, '../../.tools/payment-feasibility');
// This process reads only the sponsor key, never the test user's key.
const sponsor = Keypair.fromSecretKey(Uint8Array.from(JSON.parse(await readFile(`${output}/solana-sponsor-only.json`, 'utf8'))));
const addresses = JSON.parse(await readFile(`${output}/solana-live-addresses.json`, 'utf8'));
assert.equal(addresses.sponsor, sponsor.publicKey.toBase58());
const statePath = `${output}/${probeName}-state.json`;
let state = null;
try { state = JSON.parse(await readFile(statePath, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const save = async () => {
  await writeFile(`${statePath}.tmp`, JSON.stringify(state, null, 2) + '\n', { mode: 0o600 });
  await rename(`${statePath}.tmp`, statePath);
};
const publicState = () => {
  if (!state) return null;
  const { signed, userSigned, ...visible } = state;
  return visible;
};
let mutating = false;
async function refresh() {
  if (!state?.signature) return;
  const status = (await rpc(DEVNET_ENDPOINT, 'getSignatureStatuses', [[state.signature], { searchTransactionHistory: true }])).value[0];
  state.chainStatus = status;
  if (status?.err) { state.status = 'failed'; await save(); throw new Error(`Chain rejected transaction: ${JSON.stringify(status.err)}`); }
  if (status && ['confirmed', 'finalized'].includes(status.confirmationStatus)) {
    const receipt = await rpc(DEVNET_ENDPOINT, 'getTransaction', [state.signature, { commitment: 'confirmed', encoding: 'json', maxSupportedTransactionVersion: 0 }]);
    state.receiptCheck = checkReceipt(state, receipt);
    state.receipt = receipt;
    state.status = status.confirmationStatus;
    state.confirmedAt ??= new Date().toISOString();
  }
  await save();
}

async function handleAction(path, body) {
  if (path === '/prepare') {
    assert.equal(body.user, addresses.user, 'Only the configured dedicated test account is allowed');
    assert(!state?.signature, 'A signed submission already exists; inspect it before preparing another payment');
    const snapshot = token ? await inspectTokenAccounts(addresses, token) : null;
    const block = (await rpc(DEVNET_ENDPOINT, 'getLatestBlockhash', [{ commitment: 'confirmed' }])).value;
    state = token ? prepareTokenOrder(addresses, block, snapshot, existingAta, token) : prepareOrder(addresses, block);
    await save();
  } else if (path === '/validate') {
    assert(state && !state.signature, 'No unsigned order available');
    const tx = validateUserTransaction(state, body.reference, body.transaction);
    const height = await rpc(DEVNET_ENDPOINT, 'getBlockHeight', [{ commitment: 'confirmed' }]);
    assert(height <= state.lastValidBlockHeight, 'Blockhash expired; request a fresh wallet signature');
    state.userSigned = tx.serialize({ requireAllSignatures: false }).toString('base64');
    state.userSignature = bs58.encode(tx.signatures.find(item => item.publicKey.toBase58() === addresses.user).signature);
    state.returnedMessageSha256 = digest(tx.serializeMessage());
    state.walletSignedAt = new Date().toISOString();
    state.status = 'validated';
    await save();
  } else if (path === '/submit') {
    assert.equal(body.reference, state?.reference, 'Order reference mismatch');
    if (state.signature) { await refresh(); return { ...publicState(), reusedSubmission: true }; }
    assert.equal(state.status, 'validated', 'A validated wallet signature is required');
    const tx = validateUserTransaction(state, state.reference, state.userSigned);
    const height = await rpc(DEVNET_ENDPOINT, 'getBlockHeight', [{ commitment: 'confirmed' }]);
    assert(height <= state.lastValidBlockHeight, 'Blockhash expired; request a fresh wallet signature');
    const fee = (await rpc(DEVNET_ENDPOINT, 'getFeeForMessage', [tx.serializeMessage().toString('base64'), { commitment: 'confirmed' }])).value;
    assert(fee !== null && fee <= MAX_FEE_LAMPORTS, 'Fee unavailable or exceeds probe limit');
    tx.partialSign(sponsor);
    assert(tx.verifySignatures());
    const signed = tx.serialize().toString('base64');
    const simulation = await rpc(DEVNET_ENDPOINT, 'simulateTransaction', [signed, { encoding: 'base64', sigVerify: true, commitment: 'confirmed' }]);
    assert.equal(simulation.value.err, null, `Simulation failed: ${JSON.stringify(simulation.value)}`);
    state.simulation = simulation;
    state.estimatedFeeLamports = fee;
    state.signature = bs58.encode(tx.signature);
    state.signed = signed;
    state.status = 'signed';
    // Retain the exact signed bytes before any network submission; no automatic replacement.
    await save();
    const accepted = await rpc(DEVNET_ENDPOINT, 'sendTransaction', [signed, { encoding: 'base64', skipPreflight: false, preflightCommitment: 'confirmed', maxRetries: 0 }]);
    assert.equal(accepted, state.signature);
    state.status = 'submitted';
    state.submittedAt = new Date().toISOString();
    await save();
  } else if (path === '/replay') {
    assert.equal(body.reference, state?.reference, 'Order reference mismatch');
    assert(state?.signed, 'No stored signed submission');
    // Explicit replay of identical bytes, including recovery after an ambiguous RPC response.
    state.replaySignature = await rpc(DEVNET_ENDPOINT, 'sendTransaction', [state.signed, { encoding: 'base64', skipPreflight: false, preflightCommitment: 'confirmed', maxRetries: 0 }]);
    assert.equal(state.replaySignature, state.signature);
    state.replayedAt = new Date().toISOString();
    await save();
    await refresh();
  } else throw new Error('Unknown action');
  return publicState();
}

const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://127.0.0.1:18792').pathname;
  try {
    if (req.method === 'GET') {
      if (path === '/config') {
        res.setHeader('Content-Type', 'application/json');
        res.end(JSON.stringify({ probeName, asset: token?.symbol ?? 'SOL', paymentDescription, addresses })); return;
      }
      if (path === '/state') {
        if (!mutating) { mutating = true; try { await refresh(); } finally { mutating = false; } }
        res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(publicState())); return;
      }
      const files = { '/': ['solana-live.html', 'text/html'], '/client.js': ['solana-live-client.js', 'application/javascript'], '/web3.js': ['node_modules/@solana/web3.js/lib/index.iife.min.js', 'application/javascript'] };
      if (!files[path]) { res.writeHead(404).end(); return; }
      const file = await readFile(resolve(here, files[path][0]));
      res.setHeader('Content-Type', files[path][1]);
      res.end(path === '/' ? file.toString().replace('__PAYMENT_DESCRIPTION__', paymentDescription) : file); return;
    }
    if (req.method !== 'POST') { res.writeHead(405).end(); return; }
    if (req.headers.origin && req.headers.origin !== 'http://127.0.0.1:18792') throw new Error('Unexpected request origin');
    if (mutating) throw new Error('Another probe action is in progress');
    mutating = true;
    try {
      let raw = '';
      for await (const chunk of req) { raw += chunk; if (raw.length > 65536) throw new Error('Request too large'); }
      const result = await handleAction(path, JSON.parse(raw));
      res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(result));
    } finally { mutating = false; }
  } catch (error) {
    console.error(error.message);
    res.writeHead(400, { 'Content-Type': 'application/json' }).end(JSON.stringify({ error: error.message }));
  }
});
server.listen(18792, '127.0.0.1', () => console.log(`${probeName} on http://127.0.0.1:18792; ${paymentDescription}`));
