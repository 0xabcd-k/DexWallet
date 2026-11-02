// Read-only audit of the actual MetaMask-sponsored account activation, not an ETH payment.
import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { encodeFunctionData, decodeFunctionResult, keccak256 } from 'viem';
import { recoverAuthorizationAddress } from 'viem/utils';
import { rpc } from './rpc.mjs';
import { ENDPOINT, CHAIN_ID, ORACLE, oracleAbi, json } from './base-live-core.mjs';

assert.equal(process.argv.length, 2);
const hash = '0x782055191fab42713dd2f4e67ceb83640ed1d2f84b197bf6a7af650004862a04';
const user = '0xecd14f7586f5053cecae148ae26309c2277d86a8';
const sponsor = '0xa59c992aa84657df4ebff6f04e11a1632f6db1fa';
// Official MetaMask EIP7702StatelessDeleGator deployment; verified against Kit 2.0.0.
const implementation = '0x63c0c19a282a1b52b07dd5a65b58948a07dae32b';
const manager = '0xdb9b1e94b5b69df7e401ddbede43491141047db3';
const query = (method, params = []) => rpc(ENDPOINT, method, params, { direct: true });
assert.equal(Number(await query('eth_chainId')), CHAIN_ID);
const [transaction, receipt] = await Promise.all([
  query('eth_getTransactionByHash', [hash]), query('eth_getTransactionReceipt', [hash]),
]);
assert(transaction && receipt, 'Activation transaction not found');
assert.equal(transaction.hash, hash); assert.equal(receipt.transactionHash, hash);
assert.equal(receipt.status, '0x1'); assert.equal(transaction.type, '0x4');
assert.equal(Number(transaction.chainId), CHAIN_ID);
assert.equal(transaction.to, manager); assert.equal(transaction.value, '0x0');
assert.equal(receipt.from, transaction.from); assert.equal(receipt.to, transaction.to);
assert(![user, sponsor].includes(transaction.from), 'Expected a third-party activation fee payer');
assert.equal(transaction.authorizationList.length, 1);
const authorization = transaction.authorizationList[0];
assert.equal(Number(authorization.chainId), CHAIN_ID);
assert.equal(authorization.address, implementation); assert.equal(Number(authorization.nonce), 0);
const authority = await recoverAuthorizationAddress({ authorization: {
  ...authorization, chainId: Number(authorization.chainId), nonce: Number(authorization.nonce),
  yParity: Number(authorization.yParity),
} });
assert.equal(authority.toLowerCase(), user);
const previousBlock = `0x${(BigInt(receipt.blockNumber) - 1n).toString(16)}`;
async function snapshot(block) {
  const [userBalance, userCode, userNonce, sponsorBalance, sponsorNonce] = await Promise.all([
    query('eth_getBalance', [user, block]), query('eth_getCode', [user, block]),
    query('eth_getTransactionCount', [user, block]), query('eth_getBalance', [sponsor, block]),
    query('eth_getTransactionCount', [sponsor, block]),
  ]);
  return { block, userBalance, userCode, userNonce, sponsorBalance, sponsorNonce };
}
const [before, after, block, finalized, implementationCode] = await Promise.all([
  snapshot(previousBlock), snapshot(receipt.blockNumber),
  query('eth_getBlockByNumber', [receipt.blockNumber, false]),
  query('eth_getBlockByNumber', ['finalized', false]),
  query('eth_getCode', [implementation, receipt.blockNumber]),
]);
assert.equal(block.hash, receipt.blockHash); assert.equal(transaction.blockHash, block.hash);
assert.equal(before.userCode, '0x');
assert.equal(after.userCode, `0xef0100${implementation.slice(2)}`);
assert.equal(before.userNonce, '0x0'); assert.equal(after.userNonce, '0x1');
assert.equal(before.userBalance, '0x0'); assert.equal(after.userBalance, '0x0');
assert.equal(after.sponsorBalance, before.sponsorBalance);
assert.equal(after.sponsorNonce, before.sponsorNonce);
assert(implementationCode !== '0x');
const operatorFee = decodeFunctionResult({ abi: oracleAbi, functionName: 'getOperatorFee', data:
  await query('eth_call', [{ to: ORACLE, data: encodeFunctionData({ abi: oracleAbi,
    functionName: 'getOperatorFee', args: [BigInt(receipt.gasUsed)] }) }, receipt.blockNumber]),
});
const fees = {
  l2ExecutionFeeWei: BigInt(receipt.gasUsed) * BigInt(receipt.effectiveGasPrice),
  l1FeeWei: BigInt(receipt.l1Fee), operatorFeeWei: operatorFee,
};
fees.totalWei = fees.l2ExecutionFeeWei + fees.l1FeeWei + fees.operatorFeeWei;
const result = {
  checkedAt: new Date().toISOString(), chainId: CHAIN_ID, transactionHash: hash,
  activationStatus: BigInt(finalized.number) >= BigInt(block.number) ? 'finalized' : 'confirmed',
  paymentStatus: 'not_verified', finalizedBlock: Number(finalized.number), blockNumber: Number(block.number),
  blockTime: new Date(Number(block.timestamp) * 1000).toISOString(),
  user, sponsor, activationRelayer: transaction.from, recoveredAuthority: authority,
  implementation, implementationCodeHash: keccak256(implementationCode),
  checks: { authorizationSignerVerified: true, codeUpgradeVerified: true,
    userZeroEthPreserved: true, projectSponsorBalanceAndNonceUnchanged: true },
  before, after, fees, transaction, receipt,
};
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
await writeFile(`${root}/base-eth-upgrade-public-result.json`, json(result) + '\n');
console.log(json({ activationStatus: result.activationStatus, paymentStatus: result.paymentStatus,
  transactionHash: hash, blockNumber: result.blockNumber, finalizedBlock: result.finalizedBlock, fees, checks: result.checks }));
