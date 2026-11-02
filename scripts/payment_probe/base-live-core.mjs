import assert from 'node:assert/strict';
import { randomBytes, randomUUID } from 'node:crypto';
import { decodeEventLog, encodeFunctionData, hashTypedData, parseAbi, parseSignature, verifyTypedData } from 'viem';

export const CHAIN_ID = 84532;
export const ENDPOINT = 'https://base-sepolia-rpc.publicnode.com';
export const TOKEN = '0x036CbD53842c5426634e7929541eC2318f3dCF7e';
export const ORACLE = '0x420000000000000000000000000000000000000F';
export const AMOUNT = 10_000n; // 0.01 official test USDC.
export const MAX_FEE_WEI = 100_000_000_000_000n; // 0.0001 test ETH per probe.
export const abi = parseAbi([
  'function name() view returns (string)', 'function version() view returns (string)',
  'function decimals() view returns (uint8)', 'function DOMAIN_SEPARATOR() view returns (bytes32)',
  'function balanceOf(address) view returns (uint256)',
  'function authorizationState(address,bytes32) view returns (bool)',
  'function transferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce,uint8 v,bytes32 r,bytes32 s)',
  'event Transfer(address indexed from,address indexed to,uint256 value)',
  'event AuthorizationUsed(address indexed authorizer,bytes32 indexed nonce)',
]);
export const oracleAbi = parseAbi([
  'function getL1FeeUpperBound(uint256) view returns (uint256)',
  'function getOperatorFee(uint256) view returns (uint256)',
]);
export const domain = { name: 'USDC', version: '2', chainId: CHAIN_ID, verifyingContract: TOKEN };
export const domainTypes = [{ name: 'name', type: 'string' }, { name: 'version', type: 'string' }, { name: 'chainId', type: 'uint256' }, { name: 'verifyingContract', type: 'address' }];
export const types = { TransferWithAuthorization: [
  { name: 'from', type: 'address' }, { name: 'to', type: 'address' }, { name: 'value', type: 'uint256' },
  { name: 'validAfter', type: 'uint256' }, { name: 'validBefore', type: 'uint256' }, { name: 'nonce', type: 'bytes32' },
] };
export const json = value => JSON.stringify(value, (_, item) => typeof item === 'bigint' ? item.toString() : item, 2);
const equalAddress = (left, right) => left.toLowerCase() === right.toLowerCase();

export function prepareOrder(addresses, timestamp) {
  assert(!equalAddress(addresses.user, addresses.sponsor));
  assert(equalAddress(addresses.recipient, addresses.sponsor), 'This probe models payment into the platform account');
  const now = BigInt(timestamp);
  const typed = { domain, types: { EIP712Domain: domainTypes, ...types }, primaryType: 'TransferWithAuthorization', message: {
    from: addresses.user, to: addresses.recipient, value: AMOUNT.toString(),
    validAfter: (now - 1n).toString(), validBefore: (now + 1800n).toString(), nonce: `0x${randomBytes(32).toString('hex')}`,
  } };
  return { reference: randomUUID(), status: 'prepared', addresses, typed, typedDataHash: hashTypedData(typed), preparedAt: new Date().toISOString() };
}

export async function validateAuthorization(order, reference, signature, timestamp) {
  assert.equal(reference, order.reference, 'Order reference mismatch');
  const now = BigInt(timestamp), { message } = order.typed;
  assert(now > BigInt(message.validAfter) && now < BigInt(message.validBefore), 'Authorization outside its validity interval');
  assert(await verifyTypedData({ ...order.typed, address: order.addresses.user, signature }), 'Signature does not authorize the stored order');
  const { r, s, v, yParity } = parseSignature(signature);
  return encodeFunctionData({ abi, functionName: 'transferWithAuthorization', args: [
    message.from, message.to, BigInt(message.value), BigInt(message.validAfter), BigInt(message.validBefore), message.nonce,
    Number(v ?? BigInt(27 + yParity)), r, s,
  ] });
}

export function verifyReceipt(order, receipt, transaction) {
  assert.equal(receipt.transactionHash, order.txHash, 'Unexpected receipt');
  assert.equal(receipt.status, '0x1', 'Onchain execution failed');
  assert(equalAddress(receipt.from, order.addresses.sponsor), 'Wrong fee payer');
  assert(equalAddress(receipt.to, TOKEN), 'Wrong token contract');
  assert.equal(transaction.hash, order.txHash);
  assert.equal(Number(transaction.chainId), CHAIN_ID);
  assert(equalAddress(transaction.from, order.addresses.sponsor));
  assert(equalAddress(transaction.to, TOKEN));
  assert.equal(BigInt(transaction.value), 0n);
  assert.equal(transaction.input.toLowerCase(), order.callData.toLowerCase(), 'Unexpected executed calldata');
  const events = receipt.logs.filter(log => equalAddress(log.address, TOKEN)).flatMap(log => {
    try { return [decodeEventLog({ abi, data: log.data, topics: log.topics, strict: true })]; }
    catch { return []; } // Other token events are not transfer evidence.
  });
  const transfers = events.filter(event => event.eventName === 'Transfer');
  const authorizations = events.filter(event => event.eventName === 'AuthorizationUsed');
  assert.equal(transfers.length, 1, 'Require exactly one token transfer');
  assert.equal(authorizations.length, 1, 'Require exactly one consumed authorization');
  assert(equalAddress(transfers[0].args.from, order.addresses.user));
  assert(equalAddress(transfers[0].args.to, order.addresses.recipient));
  assert.equal(transfers[0].args.value, AMOUNT);
  assert(equalAddress(authorizations[0].args.authorizer, order.addresses.user));
  assert.equal(authorizations[0].args.nonce, order.typed.message.nonce);
  assert(receipt.l1Fee !== undefined, 'Missing Base L1 fee');
  return { transferVerified: true, authorizationEventVerified: true,
    l2ExecutionFeeWei: (BigInt(receipt.gasUsed) * BigInt(receipt.effectiveGasPrice)).toString(),
    l1FeeWei: BigInt(receipt.l1Fee).toString(),
  };
}

export function verifyBalances(before, after, fees) {
  assert.equal(BigInt(before.user.ethWei), 0n, 'Probe requires a user with zero ETH');
  assert.equal(after.user.ethWei, before.user.ethWei, 'User paid native currency');
  assert.equal(BigInt(before.user.usdcAtomic) - BigInt(after.user.usdcAtomic), AMOUNT);
  assert.equal(BigInt(after.sponsor.usdcAtomic) - BigInt(before.sponsor.usdcAtomic), AMOUNT);
  const total = BigInt(fees.l2ExecutionFeeWei) + BigInt(fees.l1FeeWei) + BigInt(fees.operatorFeeWei);
  assert.equal(BigInt(before.sponsor.ethWei) - BigInt(after.sponsor.ethWei), total, 'Sponsor balance delta differs from full network fee');
  assert(total > 0n && total <= MAX_FEE_WEI, 'Actual fee outside probe limit');
  return { userZeroEthPreserved: true, usdcMovedOnce: true, sponsorFeeVerified: true, totalFeeWei: total.toString() };
}
