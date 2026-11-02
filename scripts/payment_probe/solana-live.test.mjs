import assert from 'node:assert/strict';
import test from 'node:test';
import { Keypair, PublicKey, SystemProgram, Transaction } from '@solana/web3.js';
import { prepareOrder, validateUserTransaction, verifyReceipt } from './solana-live-core.mjs';

const user = Keypair.generate(), sponsor = Keypair.generate(), recipient = Keypair.generate();
const addresses = { user: user.publicKey.toBase58(), sponsor: sponsor.publicKey.toBase58(), recipient: recipient.publicKey.toBase58() };
const order = prepareOrder(addresses, { blockhash: Keypair.generate().publicKey.toBase58(), lastValidBlockHeight: 100 });
function returned(mutate = () => {}) {
  const tx = Transaction.from(Buffer.from(order.unsigned, 'base64'));
  mutate(tx);
  tx.signatures = [];
  tx.partialSign(user);
  return tx.serialize({ requireAllSignatures: false }).toString('base64');
}
test('accept the user-only signature for the exact server payment', () => {
  const tx = validateUserTransaction(order, order.reference, returned());
  assert.equal(tx.verifySignatures(false), true);
  assert.equal(tx.verifySignatures(true), false);
});
for (const [name, mutate] of [
  ['amount', tx => tx.instructions[0].data.writeBigUInt64LE(1001n, 4)],
  ['recipient', tx => { tx.instructions[0].keys[1].pubkey = Keypair.generate().publicKey; }],
  ['extra transfer', tx => tx.add(SystemProgram.transfer({ fromPubkey: user.publicKey, toPubkey: recipient.publicKey, lamports: 1 }))],
  ['blockhash', tx => { tx.recentBlockhash = Keypair.generate().publicKey.toBase58(); }],
  ['fee payer', tx => { tx.feePayer = user.publicKey; }],
]) test(`reject a valid user signature authorizing a changed ${name}`, () => {
  assert.throws(() => validateUserTransaction(order, order.reference, returned(mutate)), /differs from the server order/);
});
test('reject an unsigned request and a replaced order reference', () => {
  assert.throws(() => validateUserTransaction(order, order.reference, order.unsigned), /signature is missing/);
  assert.throws(() => validateUserTransaction(order, 'another-order', returned()), /Unknown or replaced order/);
});
test('reject a corrupted user signature', () => {
  const tx = Transaction.from(Buffer.from(returned(), 'base64'));
  tx.signatures.find(item => item.publicKey.equals(new PublicKey(addresses.user))).signature[0] ^= 1;
  assert.throws(() => validateUserTransaction(order, order.reference, tx.serialize({ requireAllSignatures: false, verifySignatures: false }).toString('base64')), /Invalid user signature/);
});
test('receipt must prove sender amount, recipient credit and sponsor-only fee', () => {
  const receipt = { transaction: { message: { accountKeys: [addresses.sponsor, addresses.user, addresses.recipient] } }, meta: { err: null, fee: 10000, preBalances: [100000000, 5000000, 1000000], postBalances: [99990000, 4999000, 1001000] } };
  assert.equal(verifyReceipt(order, receipt).feeLamports, 10000);
  const incorrect = structuredClone(receipt);
  incorrect.meta.postBalances[1] -= 10000;
  assert.throws(() => verifyReceipt(order, incorrect), /Sender paid more or less/);
  const failed = structuredClone(receipt);
  failed.meta.err = { InstructionError: [0, 'InsufficientFunds'] };
  assert.throws(() => verifyReceipt(order, failed), /execution failed/);
});
