import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { Keypair, SystemProgram, Transaction, PublicKey } from '@solana/web3.js';
import { createTransferCheckedInstruction, getAssociatedTokenAddressSync } from '@solana/spl-token';
import { rpc } from './rpc.mjs';

const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
await mkdir(output, { recursive: true, mode: 0o700 });
const keyPath = `${output}/solana-test-keys.json`;
let stored;
try { stored = JSON.parse(await readFile(keyPath, 'utf8')); }
catch (error) {
  if (error.code !== 'ENOENT') throw error;
  stored = Object.fromEntries(['user', 'sponsor', 'recipient'].map(role => [role, [...Keypair.generate().secretKey]]));
  await writeFile(keyPath, JSON.stringify(stored), { flag: 'wx', mode: 0o600 });
}
const keys = Object.fromEntries(Object.entries(stored).map(([role, value]) => [role, Keypair.fromSecretKey(Uint8Array.from(value))]));
const endpoint = 'https://api.devnet.solana.com';
const report = { observedAt: new Date().toISOString(), endpoint, scope: 'SDK keys, not an external wallet', addresses: Object.fromEntries(Object.entries(keys).map(([role, key]) => [role, key.publicKey.toBase58()])), checks: {} };
try {
  report.version = await rpc(endpoint, 'getVersion');
  const block = (await rpc(endpoint, 'getLatestBlockhash', [{ commitment: 'confirmed' }])).value;
  report.block = block;
  const make = instruction => new Transaction({ feePayer: keys.sponsor.publicKey, recentBlockhash: block.blockhash }).add(instruction);
  const native = make(SystemProgram.transfer({ fromPubkey: keys.user.publicKey, toPubkey: keys.recipient.publicKey, lamports: 1000 }));
  native.partialSign(keys.user);
  assert.equal(native.verifySignatures(false), true);
  assert.equal(native.verifySignatures(true), false);
  const received = Transaction.from(native.serialize({ requireAllSignatures: false }));
  assert.deepEqual(received.serializeMessage(), native.serializeMessage());
  received.partialSign(keys.sponsor);
  assert.equal(received.verifySignatures(), true);
  const altered = Transaction.from(received.serialize());
  altered.instructions[0].data.writeBigUInt64LE(1001n, 4);
  assert.equal(altered.verifySignatures(false), false);
  const changedRecipient = Transaction.from(received.serialize());
  changedRecipient.instructions[0].keys[1].pubkey = keys.sponsor.publicKey;
  assert.equal(changedRecipient.verifySignatures(false), false);
  report.checks.sol = { userOnlyAccepted: true, sponsorSignatureRequired: true, backendCanComplete: true, changedAmountRejected: true, changedRecipientRejected: true };
  // Circle's devnet USDC mint. This probe constructs an instruction, without claiming token movement.
  const mint = new PublicKey('4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU');
  const token = make(createTransferCheckedInstruction(getAssociatedTokenAddressSync(mint, keys.user.publicKey), mint, getAssociatedTokenAddressSync(mint, keys.recipient.publicKey), keys.user.publicKey, 1000n, 6));
  token.partialSign(keys.user);
  const tokenReceived = Transaction.from(token.serialize({ requireAllSignatures: false }));
  tokenReceived.partialSign(keys.sponsor);
  assert.equal(tokenReceived.verifySignatures(), true);
  report.checks.spl = { mint: mint.toBase58(), localTransferCheckedSignaturePassed: true, tokenTransferBroadcast: false };
  report.feeLamports = (await rpc(endpoint, 'getFeeForMessage', [received.serializeMessage().toString('base64'), { commitment: 'confirmed' }])).value;
  report.balances = {};
  for (const [role, address] of Object.entries(report.addresses)) report.balances[role] = (await rpc(endpoint, 'getBalance', [address, { commitment: 'confirmed' }])).value;
  if (process.argv.includes('--airdrop')) {
    try { report.airdropSignature = await rpc(endpoint, 'requestAirdrop', [report.addresses.sponsor, 100000000, { commitment: 'confirmed' }]); }
    catch (error) { report.airdropError = error.message; }
  }
  // Read-only simulation, no transaction broadcast. Unfunded accounts are recorded as a limitation.
  report.simulation = await rpc(endpoint, 'simulateTransaction', [received.serialize().toString('base64'), { encoding: 'base64', sigVerify: true, commitment: 'confirmed' }]);
} catch (error) { report.error = error.message; process.exitCode = 1; }
await writeFile(`${output}/solana-result.json`, JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
