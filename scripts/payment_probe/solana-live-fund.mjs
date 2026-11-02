import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { setTimeout } from 'node:timers/promises';
import { Keypair, SystemProgram, Transaction } from '@solana/web3.js';
import bs58 from 'bs58';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT } from './solana-live-core.mjs';

const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const fixture = JSON.parse(await readFile(`${output}/solana-test-keys.json`, 'utf8'));
const keys = Object.fromEntries(Object.entries(fixture).map(([role, bytes]) => [role, Keypair.fromSecretKey(Uint8Array.from(bytes))]));
const addresses = Object.fromEntries(Object.entries(keys).map(([role, key]) => [role, key.publicKey.toBase58()]));
for (const [name, value] of [['solana-sponsor-only.json', fixture.sponsor], ['solana-live-addresses.json', addresses]]) {
  try { await writeFile(`${output}/${name}`, JSON.stringify(value), { flag: 'wx', mode: 0o600 }); }
  catch (error) {
    if (error.code !== 'EEXIST') throw error;
    assert.deepEqual(JSON.parse(await readFile(`${output}/${name}`, 'utf8')), value, 'Existing fixture differs');
  }
}
const statePath = `${output}/solana-live-funding.json`;
let state;
try { state = JSON.parse(await readFile(statePath, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
if (!state) {
  const balances = {};
  for (const [role, address] of Object.entries(addresses)) balances[role] = (await rpc(DEVNET_ENDPOINT, 'getBalance', [address, { commitment: 'confirmed' }])).value;
  const block = (await rpc(DEVNET_ENDPOINT, 'getLatestBlockhash', [{ commitment: 'confirmed' }])).value;
  const tx = new Transaction({ feePayer: keys.sponsor.publicKey, recentBlockhash: block.blockhash });
  const funding = { user: Math.max(0, 5000000 - balances.user), recipient: Math.max(0, 1000000 - balances.recipient) };
  for (const [role, lamports] of Object.entries(funding)) if (lamports) tx.add(SystemProgram.transfer({ fromPubkey: keys.sponsor.publicKey, toPubkey: keys[role].publicKey, lamports }));
  if (!tx.instructions.length) { console.log('Test accounts already funded; sponsor-only configuration ready.'); process.exit(0); }
  assert(balances.sponsor >= funding.user + funding.recipient + 100000, 'Sponsor needs Devnet test SOL');
  tx.sign(keys.sponsor);
  state = { scope: 'Test account preparation, not the wallet payment', observedAt: new Date().toISOString(), addresses, balancesBefore: balances, funding, ...block, signature: bs58.encode(tx.signature), signed: tx.serialize().toString('base64') };
  await writeFile(statePath, JSON.stringify(state, null, 2), { flag: 'wx', mode: 0o600 });
}
if (!state.receipt) {
  const known = (await rpc(DEVNET_ENDPOINT, 'getSignatureStatuses', [[state.signature], { searchTransactionHistory: true }])).value[0];
  if (!known) {
    const accepted = await rpc(DEVNET_ENDPOINT, 'sendTransaction', [state.signed, { encoding: 'base64', skipPreflight: false, preflightCommitment: 'confirmed', maxRetries: 0 }]);
    assert.equal(accepted, state.signature);
    console.log('Funding transaction submitted:', accepted);
  }
  for (let attempt = 0; attempt < 20; attempt++) {
    const status = (await rpc(DEVNET_ENDPOINT, 'getSignatureStatuses', [[state.signature], { searchTransactionHistory: true }])).value[0];
    if (status?.err) throw new Error(JSON.stringify(status.err));
    if (status && ['confirmed', 'finalized'].includes(status.confirmationStatus)) {
      state.receipt = await rpc(DEVNET_ENDPOINT, 'getTransaction', [state.signature, { commitment: 'confirmed', encoding: 'json', maxSupportedTransactionVersion: 0 }]);
      assert.equal(state.receipt.meta.err, null);
      state.confirmationStatus = status.confirmationStatus;
      await writeFile(statePath, JSON.stringify(state, null, 2), { mode: 0o600 });
      break;
    }
    await setTimeout(1500);
  }
  assert(state.receipt, 'Funding confirmation pending; rerun to inspect the same signed transaction');
}
const { signed, ...report } = state;
console.log(JSON.stringify(report, null, 2));
