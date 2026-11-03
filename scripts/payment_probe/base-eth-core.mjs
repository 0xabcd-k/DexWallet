// Narrow Base Sepolia experiment; this is not a general permission validator.
import assert from 'node:assert/strict';
import { decodeAbiParameters, encodeAbiParameters, encodeFunctionData, encodePacked, hashTypedData, maxUint256, parseAbi, parseAbiParameters, verifyTypedData, zeroHash } from 'viem';

export const ETH_AMOUNT = 1_000_000_000_000n;
export const MANAGER = '0xdb9B1e94B5b69Df7e401DDbedE43491141047dB3';
export const ACCOUNT_CODE = '0xef010063c0c19a282a1b52b07dd5a65b58948a07dae32b';
export const CHAIN_ID = 84532;
// Official @metamask/delegation-deployments 2.0.0, framework 1.3.0.
export const ENFORCERS = {
  period: '0x9BC0FAf4Aca5AE429F4c06aEEaC517520CB16BD9',
  calldata: '0x99F2e9bF15ce5eC84685604836F71aB835DBBdED',
  timestamp: '0x1046bb45C8d673d4ea75321280DB34899413c069',
  redeemer: '0xE144b0b2618071B4E56f746313528a669c7E65c5',
  target: '0x7F20f61b1f09b08D970938F6fa563634d65c4EeB',
  nonce: '0xDE4f2FAC4B3D87A1d9953Ca5FC09FCa7F366254f',
};
export const delegationParameters = parseAbiParameters('(address delegate,address delegator,bytes32 authority,(address enforcer,bytes terms,bytes args)[] caveats,uint256 salt,bytes signature)[]');
export const managerAbi = parseAbi([
  'error InvalidDelegate()',
  'function redeemDelegations(bytes[] permissionContexts,bytes32[] modes,bytes[] executionCallDatas)',
  'event RedeemedDelegation(address indexed rootDelegator,address indexed redeemer,(address delegate,address delegator,bytes32 authority,(address enforcer,bytes terms,bytes args)[] caveats,uint256 salt,bytes signature) delegation)',
]);
export const types = {
  Caveat: [{ name: 'enforcer', type: 'address' }, { name: 'terms', type: 'bytes' }],
  Delegation: [{ name: 'delegate', type: 'address' }, { name: 'delegator', type: 'address' },
    { name: 'authority', type: 'bytes32' }, { name: 'caveats', type: 'Caveat[]' }, { name: 'salt', type: 'uint256' }],
};
export const sameAddress = (a, b) => a.toLowerCase() === b.toLowerCase();
export const typedDelegation = delegation => ({
  domain: { name: 'DelegationManager', version: '1', chainId: CHAIN_ID, verifyingContract: MANAGER },
  primaryType: 'Delegation', types, message: delegation,
});
export function expectedCaveats(intent) {
  return [
    { enforcer: ENFORCERS.period, terms: encodePacked(['uint256', 'uint256', 'uint256'], [ETH_AMOUNT, maxUint256, BigInt(intent.start)]) },
    { enforcer: ENFORCERS.calldata, terms: '0x' },
    { enforcer: ENFORCERS.timestamp, terms: encodePacked(['uint128', 'uint128'], [0n, BigInt(intent.expiry)]) },
    { enforcer: ENFORCERS.redeemer, terms: intent.sponsor.toLowerCase() },
    { enforcer: ENFORCERS.target, terms: intent.sponsor.toLowerCase() },
    // Revocation nonce, not a transaction nonce and not a one-use guarantee.
    { enforcer: ENFORCERS.nonce, terms: zeroHash },
  ].map(caveat => ({ ...caveat, args: '0x' }));
}
export function paymentData(context, target, amount = ETH_AMOUNT, data = '0x') {
  return encodeFunctionData({ abi: managerAbi, functionName: 'redeemDelegations', args: [
    [context], [zeroHash], [encodePacked(['address', 'uint256', 'bytes'], [target, amount, data])],
  ] });
}
export async function validateGrant(grant, intent, timestamp) {
  assert.equal(Number(grant.chainId), CHAIN_ID, 'Wrong chain');
  assert(sameAddress(grant.from, intent.user) && sameAddress(grant.to, intent.sponsor), 'Wrong accounts');
  assert(sameAddress(grant.delegationManager, MANAGER), 'Wrong delegation manager');
  assert.deepEqual(grant.dependencies, [], 'Account setup dependencies require separate review');
  assert.equal(intent.expiry - intent.start, 3600, 'Only the fixed one-hour experiment is allowed');
  assert(Number(timestamp) >= intent.start && Number(timestamp) < intent.expiry, 'Grant outside validity interval');
  assert.equal(grant.permission.type, 'native-token-allowance');
  assert.equal(grant.permission.isAdjustmentAllowed, false);
  assert.equal(BigInt(grant.permission.data.allowanceAmount), ETH_AMOUNT, 'Wrong allowance');
  assert.equal(grant.permission.data.startTime, intent.start);
  assert.deepEqual(grant.rules, [
    { type: 'expiry', data: { timestamp: intent.expiry } },
    { type: 'redeemer', data: { addresses: [intent.sponsor.toLowerCase()] } },
    { type: 'payee', data: { addresses: [intent.sponsor.toLowerCase()] } },
  ], 'Unexpected permission rules');
  const delegations = decodeAbiParameters(delegationParameters, grant.context)[0];
  assert.equal(delegations.length, 1, 'Only a direct delegation is accepted');
  assert.equal(encodeAbiParameters(delegationParameters, [delegations]).toLowerCase(), grant.context.toLowerCase(), 'Noncanonical context');
  const delegation = delegations[0];
  assert(sameAddress(delegation.delegator, intent.user) && sameAddress(delegation.delegate, intent.sponsor), 'Context accounts differ from intent');
  assert.equal(delegation.authority, `0x${'ff'.repeat(32)}`, 'Unexpected parent delegation');
  const normalized = caveats => caveats.map(c => ({ enforcer: c.enforcer.toLowerCase(), terms: c.terms.toLowerCase(), args: c.args.toLowerCase() }));
  assert.deepEqual(normalized(delegation.caveats), normalized(expectedCaveats(intent)), 'Signed caveats differ from intent');
  const typed = typedDelegation(delegation);
  assert(await verifyTypedData({ ...typed, address: intent.user, signature: delegation.signature }), 'Invalid user signature');
  return { data: paymentData(grant.context, intent.sponsor), typedDataHash: hashTypedData(typed), signatureVerified: true };
}
