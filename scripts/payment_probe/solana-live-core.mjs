import assert from 'node:assert/strict';
import { createHash, randomUUID } from 'node:crypto';
import { PublicKey, SystemProgram, Transaction } from '@solana/web3.js';

export const DEVNET_ENDPOINT = 'https://api.devnet.solana.com';
export const PAYMENT_LAMPORTS = 1000;
export const MAX_FEE_LAMPORTS = 20000;
export const digest = bytes => createHash('sha256').update(bytes).digest('hex');

export function prepareOrder(addresses, block) {
  const transaction = new Transaction({ feePayer: new PublicKey(addresses.sponsor), recentBlockhash: block.blockhash }).add(
    SystemProgram.transfer({ fromPubkey: new PublicKey(addresses.user), toPubkey: new PublicKey(addresses.recipient), lamports: PAYMENT_LAMPORTS }),
  );
  return {
    reference: randomUUID(), preparedAt: new Date().toISOString(), network: 'solana-devnet',
    addresses, amountLamports: PAYMENT_LAMPORTS, ...block,
    unsigned: transaction.serialize({ requireAllSignatures: false }).toString('base64'),
    messageSha256: digest(transaction.serializeMessage()), status: 'prepared',
  };
}

export function validateUserTransaction(order, reference, encoded) {
  assert.equal(reference, order.reference, 'Unknown or replaced order');
  const expected = Transaction.from(Buffer.from(order.unsigned, 'base64'));
  const received = Transaction.from(Buffer.from(encoded, 'base64'));
  assert.deepEqual(received.serializeMessage(), expected.serializeMessage(), 'Wallet message differs from the server order');
  const user = received.signatures.find(item => item.publicKey.toBase58() === order.addresses.user);
  const sponsor = received.signatures.find(item => item.publicKey.toBase58() === order.addresses.sponsor);
  assert(user?.signature, 'User signature is missing');
  assert.equal(sponsor?.signature, null, 'Expected a user-only signature');
  assert.equal(received.verifySignatures(false), true, 'Invalid user signature');
  return received;
}

export function verifyReceipt(order, receipt) {
  assert(receipt?.meta, 'Confirmed transaction details unavailable');
  assert.equal(receipt.meta.err, null, 'Transaction execution failed');
  const keys = receipt.transaction.message.accountKeys;
  const delta = address => {
    const index = keys.indexOf(address);
    assert(index >= 0, 'Expected account missing from receipt');
    return receipt.meta.postBalances[index] - receipt.meta.preBalances[index];
  };
  assert.equal(keys[0], order.addresses.sponsor, 'Unexpected fee payer');
  assert.equal(delta(order.addresses.user), -order.amountLamports, 'Sender paid more or less than the transfer amount');
  assert.equal(delta(order.addresses.recipient), order.amountLamports, 'Recipient amount mismatch');
  assert.equal(delta(order.addresses.sponsor), -receipt.meta.fee, 'Sponsor fee mismatch');
  assert(receipt.meta.fee <= MAX_FEE_LAMPORTS, 'Fee exceeds probe limit');
  return { senderDelta: delta(order.addresses.user), recipientDelta: delta(order.addresses.recipient), sponsorDelta: delta(order.addresses.sponsor), feeLamports: receipt.meta.fee, feePayer: keys[0] };
}
