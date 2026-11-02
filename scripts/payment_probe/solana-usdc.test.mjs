import assert from 'node:assert/strict';
import test from 'node:test';
import { Keypair, Transaction } from '@solana/web3.js';
import { TOKEN_PROGRAM_ID } from '@solana/spl-token';
import { validateUserTransaction } from './solana-live-core.mjs';
import { USDC_MINT, prepareUsdcOrder, validateUsdcSnapshot, verifyUsdcReceipt } from './solana-usdc-core.mjs';

const user = Keypair.generate(), sponsor = Keypair.generate(), recipient = Keypair.generate();
const addresses = { user: user.publicKey.toBase58(), sponsor: sponsor.publicKey.toBase58(), recipient: recipient.publicKey.toBase58() };
const tokenProgram = TOKEN_PROGRAM_ID.toBase58();
const rent = 2039280;
const block = { blockhash: Keypair.generate().publicKey.toBase58(), lastValidBlockHeight: 100 };
function account(owner, amount) {
  return { owner: tokenProgram, data: { parsed: { type: 'account', info: {
    owner, mint: USDC_MINT, state: 'initialized', tokenAmount: { amount, decimals: 6 },
  } } } };
}
function snapshot(existing = false) {
  return { context: { slot: 1 }, value: [
    { owner: tokenProgram, data: { parsed: { type: 'mint', info: { isInitialized: true, decimals: 6 } } } },
    account(addresses.user, '20000000'), existing ? account(addresses.recipient, '10000') : null,
  ] };
}
function order(existing = false) {
  return prepareUsdcOrder(addresses, block, validateUsdcSnapshot(addresses, snapshot(existing), rent), existing);
}
function signed(order, mutate = () => {}) {
  const tx = Transaction.from(Buffer.from(order.unsigned, 'base64'));
  mutate(tx); tx.signatures = []; tx.partialSign(user);
  return tx.serialize({ requireAllSignatures: false }).toString('base64');
}
test('both ATA cases allow user-only signing and keep platform signature required', () => {
  for (const existing of [false, true]) {
    const prepared = order(existing);
    const tx = validateUserTransaction(prepared, prepared.reference, signed(prepared));
    assert.equal(tx.verifySignatures(false), true);
    assert.equal(tx.verifySignatures(true), false);
    assert.equal(prepared.estimatedAtaRentLamports, existing ? 0 : rent);
  }
});
for (const [label, mutate] of [
  ['USDC amount', tx => tx.instructions[1].data.writeBigUInt64LE(20000n, 1)],
  ['mint', tx => { tx.instructions[1].keys[1].pubkey = Keypair.generate().publicKey; }],
  ['decimals', tx => { tx.instructions[1].data[9] = 9; }],
  ['destination token account', tx => { tx.instructions[1].keys[2].pubkey = Keypair.generate().publicKey; }],
  ['ATA rent payer', tx => { tx.instructions[0].keys[0].pubkey = user.publicKey; }],
  ['ATA recipient owner', tx => { tx.instructions[0].keys[2].pubkey = Keypair.generate().publicKey; }],
]) test(`reject valid user signature with modified ${label}`, () => {
  const prepared = order();
  assert.throws(() => validateUserTransaction(prepared, prepared.reference, signed(prepared, mutate)), /differs from the server order/);
});
test('require actual Circle mint, correct authority, sufficient and unfrozen tokens before signing', () => {
  for (const [mutate, expected] of [
    [s => { s.value[0].owner = addresses.user; }, /mint program/],
    [s => { s.value[0].data.parsed.info.decimals = 9; }, /mint decimals/],
    [s => { s.value[1] = null; }, /official faucet/],
    [s => { s.value[1].data.parsed.info.tokenAmount.amount = '9999'; }, /Insufficient/],
    [s => { s.value[1].data.parsed.info.state = 'frozen'; }, /frozen/],
    [s => { s.value[1].data.parsed.info.owner = addresses.recipient; }, /authority/],
    [s => { s.value[2] = account(addresses.user, '0'); }, /authority/],
  ]) {
    const input = snapshot(); mutate(input);
    assert.throws(() => validateUsdcSnapshot(addresses, input, rent), expected);
  }
  assert.throws(() => validateUsdcSnapshot(addresses, snapshot(), 4000000), /rent exceeds/);
});
test('do not accidentally run the existing-ATA scenario as a new-ATA scenario', () => {
  assert.throws(() => prepareUsdcOrder(addresses, block, validateUsdcSnapshot(addresses, snapshot(true), rent), false), /requested test case/);
});
function receipt(prepared, existing) {
  const keys = [addresses.sponsor, addresses.user, prepared.tokenAccounts.user, prepared.tokenAccounts.recipient, addresses.recipient];
  const token = (i, amount, owner) => ({ accountIndex: i, mint: USDC_MINT, owner, programId: tokenProgram, uiTokenAmount: { amount, decimals: 6 } });
  return { transaction: { message: { accountKeys: keys } }, meta: {
    err: null, fee: 10000,
    preBalances: [100000000, 5000000, rent, existing ? rent : 0, 1000000],
    postBalances: [100000000 - 10000 - (existing ? 0 : rent), 5000000, rent, rent, 1000000],
    preTokenBalances: [token(2, '20000000', addresses.user), ...(existing ? [token(3, '10000', addresses.recipient)] : [])],
    postTokenBalances: [token(2, '19990000', addresses.user), token(3, existing ? '20000' : '10000', addresses.recipient)],
  } };
}
test('receipt proves USDC movement and sponsor funding of new ATA, then zero additional rent', () => {
  for (const existing of [false, true]) {
    const prepared = order(existing), result = verifyUsdcReceipt(prepared, receipt(prepared, existing));
    assert.equal(result.senderTokenDeltaAtomic, '-10000');
    assert.equal(result.recipientTokenDeltaAtomic, '10000');
    assert.equal(result.userSolDeltaLamports, 0);
    assert.equal(result.ataRentLamports, existing ? 0 : rent);
  }
});
test('do not report success from native SOL movement without the expected token credit', () => {
  const prepared = order();
  for (const [mutate, expected] of [
    [r => { r.meta.postTokenBalances[1].uiTokenAmount.amount = '9999'; }, /recipient credit/],
    [r => { r.meta.postTokenBalances[1].mint = addresses.user; }, /mint mismatch/],
    [r => { r.meta.postTokenBalances[1].owner = addresses.user; }, /authority mismatch/],
    [r => { r.meta.postBalances[1] -= 10000; }, /User paid native SOL/],
    [r => { r.meta.postBalances[0] -= 1; }, /exactly fee plus ATA rent/],
    [r => { r.meta.err = { InstructionError: [1, 'InsufficientFunds'] }; }, /execution failed/],
  ]) {
    const input = receipt(prepared, false); mutate(input);
    assert.throws(() => verifyUsdcReceipt(prepared, input), expected);
  }
});
