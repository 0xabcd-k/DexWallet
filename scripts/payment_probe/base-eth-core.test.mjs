import assert from 'node:assert/strict';
import { test } from 'node:test';
import { decodeFunctionData, encodeAbiParameters, zeroHash } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { CHAIN_ID, ETH_AMOUNT, MANAGER, delegationParameters, expectedCaveats, managerAbi, typedDelegation, validateGrant } from './base-eth-core.mjs';

// Public, deterministic, unfunded fixtures. These signatures are not wallet evidence.
const user = privateKeyToAccount(`0x${'1'.padStart(64, '0')}`);
const sponsor = privateKeyToAccount(`0x${'2'.padStart(64, '0')}`);
const start = 1_700_000_000;
const intent = { user: user.address, sponsor: sponsor.address, start, expiry: start + 3600 };
async function fixture(changeDelegation = () => {}, signer = user) {
  const delegation = { delegate: sponsor.address, delegator: user.address, authority: `0x${'ff'.repeat(32)}`,
    caveats: expectedCaveats(intent), salt: 1n, signature: '0x' };
  changeDelegation(delegation);
  delegation.signature = await signer.signTypedData(typedDelegation(delegation));
  return { chainId: `0x${CHAIN_ID.toString(16)}`, from: user.address, to: sponsor.address, delegationManager: MANAGER,
    dependencies: [], context: encodeAbiParameters(delegationParameters, [[delegation]]),
    permission: { type: 'native-token-allowance', isAdjustmentAllowed: false, data: { allowanceAmount: ETH_AMOUNT.toString(), startTime: start } },
    rules: [{ type: 'expiry', data: { timestamp: intent.expiry } },
      { type: 'redeemer', data: { addresses: [sponsor.address.toLowerCase()] } },
      { type: 'payee', data: { addresses: [sponsor.address.toLowerCase()] } }],
  };
}
test('valid bounded grant produces exactly one native transfer execution', async () => {
  const grant = await fixture();
  const result = await validateGrant(grant, intent, start + 1);
  const call = decodeFunctionData({ abi: managerAbi, data: result.data });
  assert.equal(call.functionName, 'redeemDelegations');
  assert.deepEqual(call.args[0], [grant.context]);
  assert.deepEqual(call.args[1], [zeroHash]);
  assert.equal(call.args[2].length, 1);
  const execution = call.args[2][0];
  assert.equal(execution.slice(0, 42), sponsor.address.toLowerCase());
  assert.equal(BigInt(`0x${execution.slice(42)}`), ETH_AMOUNT);
  assert.equal(execution.length, 2 + (20 + 32) * 2, 'Native transfer must not include arbitrary calldata');
});
for (const [name, change] of [
  ['wrong chain', grant => { grant.chainId = '0x1'; }],
  ['wrong claimed account', grant => { grant.from = sponsor.address; }],
  ['wrong manager', grant => { grant.delegationManager = sponsor.address; }],
  ['changed claimed amount', grant => { grant.permission.data.allowanceAmount = '1'; }],
  ['wider recipient list', grant => { grant.rules[2].data.addresses.push(user.address); }],
  ['unexpected setup dependency', grant => { grant.dependencies.push({ type: 'unknown' }); }],
]) test(`rejects ${name}`, async () => {
  const grant = await fixture(); change(grant);
  await assert.rejects(validateGrant(grant, intent, start + 1));
});
for (const [name, change] of [
  ['unsigned target restriction', delegation => { delegation.caveats = delegation.caveats.filter(c => c.enforcer !== expectedCaveats(intent)[4].enforcer); }],
  ['different signed recipient', delegation => { delegation.caveats[4].terms = user.address; }],
  ['arbitrary calldata', delegation => { delegation.caveats[1].terms = '0x1234'; }],
  ['modified unsigned caveat arguments', delegation => { delegation.caveats[0].args = '0x01'; }],
  ['inherited authority', delegation => { delegation.authority = zeroHash; }],
]) test(`rejects ${name} even with a valid user signature`, async () => {
  await assert.rejects(validateGrant(await fixture(change), intent, start + 1));
});
test('rejects a different signer and expired or premature grants', async () => {
  await assert.rejects(validateGrant(await fixture(() => {}, sponsor), intent, start + 1), /Invalid user signature/);
  const grant = await fixture();
  await assert.rejects(validateGrant(grant, intent, start - 1), /validity interval/);
  await assert.rejects(validateGrant(grant, intent, intent.expiry), /validity interval/);
});
