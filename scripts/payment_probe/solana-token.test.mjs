import assert from 'node:assert/strict';
import test from 'node:test';
import { Keypair, Transaction } from '@solana/web3.js';
import { TOKEN_PROGRAM_ID, TOKEN_2022_PROGRAM_ID } from '@solana/spl-token';
import { USDC, USDT_FIXTURE as asset, MAINNET_USDT_MINT, probeToken } from './solana-token-assets.mjs';
import { validateTokenSnapshot, prepareTokenOrder, verifyTokenReceipt } from './solana-token-core.mjs';
import { validateUserTransaction } from './solana-live-core.mjs';

const user = Keypair.generate(), sponsor = Keypair.generate(), recipient = Keypair.generate();
const addresses = { user: user.publicKey.toBase58(), sponsor: sponsor.publicKey.toBase58(), recipient: recipient.publicKey.toBase58() };
const rent = 1488440;
function snapshot() {
  const account = owner => ({ owner: TOKEN_PROGRAM_ID.toBase58(), data: { parsed: { type: 'account', info: {
    mint: asset.mint, owner, state: 'initialized', tokenAmount: { amount: '20000', decimals: 6 },
  } } } });
  return { context: { slot: 1 }, value: [
    { owner: TOKEN_PROGRAM_ID.toBase58(), data: { parsed: { type: 'mint', info: { isInitialized: true, decimals: 6 } } } },
    account(addresses.user), account(addresses.recipient),
  ] };
}
function order() {
  return prepareTokenOrder(addresses, { blockhash: Keypair.generate().publicKey.toBase58(), lastValidBlockHeight: 100 },
    validateTokenSnapshot(addresses, snapshot(), rent, asset), true, asset);
}
test('fixture has a distinct asset identity; actual USDT is not accepted by the Devnet selector', () => {
  assert.equal(probeToken('USDT-FIXTURE'), asset);
  assert.notEqual(asset.mint, MAINNET_USDT_MINT);
  assert.throws(() => probeToken('USDT'), /Unsupported/);
  assert.throws(() => verifyTokenReceipt(order(), null, USDC), /Unexpected probe asset/);
});
test('reject frozen source or destination, wrong mint, Token-2022 and insufficient token balance', () => {
  for (const [mutate, expected] of [
    [s => { s.value[1].data.parsed.info.state = 'frozen'; }, /frozen/],
    [s => { s.value[2].data.parsed.info.state = 'frozen'; }, /frozen/],
    [s => { s.value[1].data.parsed.info.mint = USDC.mint; }, /mint/],
    [s => { s.value[2].data.parsed.info.mint = MAINNET_USDT_MINT; }, /mint/],
    [s => { s.value[0].owner = TOKEN_2022_PROGRAM_ID.toBase58(); }, /program/],
    [s => { s.value[1].data.parsed.info.tokenAmount.amount = '9999'; }, /Insufficient/],
  ]) {
    const input = snapshot(); mutate(input);
    assert.throws(() => validateTokenSnapshot(addresses, input, rent, asset), expected);
  }
});
test('fixture payment accepts only the exact server message signed by the user', () => {
  const prepared = order();
  for (const mutation of [null, 'mint', 'amount', 'destination', 'decimals']) {
    const tx = Transaction.from(Buffer.from(prepared.unsigned, 'base64'));
    if (mutation === 'mint') tx.instructions[1].keys[1].pubkey = Keypair.generate().publicKey;
    if (mutation === 'amount') tx.instructions[1].data.writeBigUInt64LE(10001n, 1);
    if (mutation === 'destination') tx.instructions[1].keys[2].pubkey = Keypair.generate().publicKey;
    if (mutation === 'decimals') tx.instructions[1].data[9] = 9;
    tx.signatures = []; tx.partialSign(user);
    const encoded = tx.serialize({ requireAllSignatures: false }).toString('base64');
    if (mutation) assert.throws(() => validateUserTransaction(prepared, prepared.reference, encoded), /differs from the server order/);
    else {
      const validated = validateUserTransaction(prepared, prepared.reference, encoded);
      assert.equal(validated.verifySignatures(false), true);
      assert.equal(validated.verifySignatures(true), false);
    }
  }
});
