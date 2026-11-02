import assert from 'node:assert/strict';
import test from 'node:test';
import { privateKeyToAccount } from 'viem/accounts';
import { encodeAbiParameters, encodeEventTopics, toHex } from 'viem';
import { AMOUNT, CHAIN_ID, TOKEN, abi, prepareOrder, validateAuthorization, verifyReceipt, verifyBalances } from './base-live-core.mjs';

// Deterministic offline fixtures, unrelated to funded wallet keys.
const user = privateKeyToAccount(toHex(1n, { size: 32 }));
const sponsor = privateKeyToAccount(toHex(2n, { size: 32 }));
const addresses = { user: user.address, sponsor: sponsor.address, recipient: sponsor.address };
const timestamp = 1_800_000_000n;
const order = () => prepareOrder(addresses, timestamp);

test('the saved USDC authorization can be relayed by the separate sponsor', async () => {
  const o = order(), signature = await user.signTypedData(o.typed);
  const data = await validateAuthorization(o, o.reference, signature, timestamp);
  assert.match(data, /^0x[0-9a-f]+$/);
  assert.equal(o.typed.message.value, AMOUNT.toString());
  assert.notEqual(o.typed.message.from, addresses.sponsor);
});
for (const field of ['value', 'to', 'nonce', 'validBefore', 'chainId', 'verifyingContract']) {
  test(`reject wallet authorization with changed ${field}`, async () => {
    const o = order(), changed = structuredClone(o.typed);
    if (field === 'chainId') changed.domain.chainId = 8453;
    else if (field === 'verifyingContract') changed.domain.verifyingContract = user.address;
    else if (field === 'to') changed.message.to = user.address;
    else if (field === 'nonce') changed.message.nonce = toHex(42n, { size: 32 });
    else changed.message[field] = (BigInt(changed.message[field]) + 1n).toString();
    const signature = await user.signTypedData(changed);
    await assert.rejects(validateAuthorization(o, o.reference, signature, timestamp), /stored order/);
  });
}
test('reject wrong signer, business reference and expired or not-yet-valid authorization', async () => {
  const o = order(), signature = await user.signTypedData(o.typed);
  await assert.rejects(validateAuthorization(o, 'other', signature, timestamp), /reference/);
  await assert.rejects(validateAuthorization(o, o.reference, await sponsor.signTypedData(o.typed), timestamp), /stored order/);
  for (const time of [o.typed.message.validAfter, o.typed.message.validBefore]) {
    await assert.rejects(validateAuthorization(o, o.reference, signature, time), /validity/);
  }
});
function receiptFixture() {
  const o = { ...order(), txHash: toHex(99n, { size: 32 }), callData: '0x1234' };
  const log = (eventName, args, value = '0x') => ({ address: TOKEN, topics: encodeEventTopics({ abi, eventName, args }), data: value });
  const receipt = { transactionHash: o.txHash, status: '0x1', from: sponsor.address, to: TOKEN, gasUsed: '0x64', effectiveGasPrice: '0x2', l1Fee: '0x32', logs: [
    log('Transfer', { from: user.address, to: sponsor.address }, encodeAbiParameters([{ type: 'uint256' }], [AMOUNT])),
    log('AuthorizationUsed', { authorizer: user.address, nonce: o.typed.message.nonce }),
  ] };
  const transaction = { hash: o.txHash, chainId: toHex(CHAIN_ID), from: sponsor.address, to: TOKEN, value: '0x0', input: o.callData };
  return { o, receipt, transaction };
}
test('require successful exact call, official Transfer and matching AuthorizationUsed event', () => {
  const { o, receipt, transaction } = receiptFixture();
  assert.equal(verifyReceipt(o, receipt, transaction).l2ExecutionFeeWei, '200');
  for (const mutate of [
    r => { r.status = '0x0'; }, r => { r.from = user.address; }, r => { r.to = user.address; },
    r => { r.logs.pop(); }, r => { r.logs[0].address = user.address; },
    r => { r.logs[0].data = encodeAbiParameters([{ type: 'uint256' }], [AMOUNT + 1n]); },
    r => { r.logs[1].topics[2] = toHex(17n, { size: 32 }); }, r => { delete r.l1Fee; },
  ]) { const bad = structuredClone(receipt); mutate(bad); assert.throws(() => verifyReceipt(o, bad, transaction)); }
  assert.throws(() => verifyReceipt(o, receipt, { ...transaction, input: '0x4321' }), /calldata/);
});
test('zero user ETH, exact USDC movement and all fee components must reconcile', () => {
  const before = { user: { ethWei: '0', usdcAtomic: '20000' }, sponsor: { ethWei: '1000', usdcAtomic: '0' } };
  const after = { user: { ethWei: '0', usdcAtomic: '10000' }, sponsor: { ethWei: '743', usdcAtomic: '10000' } };
  const fees = { l2ExecutionFeeWei: '200', l1FeeWei: '50', operatorFeeWei: '7' };
  assert.equal(verifyBalances(before, after, fees).totalFeeWei, '257');
  assert.throws(() => verifyBalances(before, after, { ...fees, operatorFeeWei: '0' }), /fee/);
  for (const mutate of [
    b => { b.user.ethWei = '1'; }, b => { b.user.usdcAtomic = '0'; }, b => { b.sponsor.usdcAtomic = '0'; },
  ]) { const bad = structuredClone(after); mutate(bad); assert.throws(() => verifyBalances(before, bad, fees)); }
});
