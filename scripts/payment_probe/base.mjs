import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import { decodeFunctionResult, encodeFunctionData, hashDomain, keccak256, parseAbi, stringToHex, verifyTypedData } from 'viem';
import { rpc } from './rpc.mjs';

const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
await mkdir(output, { recursive: true, mode: 0o700 });
let secret;
try { secret = JSON.parse(await readFile(`${output}/base-test-keys.json`, 'utf8')); }
catch (error) {
  if (error.code !== 'ENOENT') throw error;
  secret = { user: generatePrivateKey(), sponsor: generatePrivateKey() };
  await writeFile(`${output}/base-test-keys.json`, JSON.stringify(secret), { flag: 'wx', mode: 0o600 });
}
const user = privateKeyToAccount(secret.user), sponsor = privateKeyToAccount(secret.sponsor);
const token = '0x036CbD53842c5426634e7929541eC2318f3dCF7e';
const chainId = 84532;
const endpoint = 'https://base-sepolia-rpc.publicnode.com';
const domain = { name: 'USDC', version: '2', chainId, verifyingContract: token };
const types = { TransferWithAuthorization: [
  { name: 'from', type: 'address' }, { name: 'to', type: 'address' }, { name: 'value', type: 'uint256' },
  { name: 'validAfter', type: 'uint256' }, { name: 'validBefore', type: 'uint256' }, { name: 'nonce', type: 'bytes32' },
] };
const now = BigInt(Math.floor(Date.now() / 1000));
const message = { from: user.address, to: sponsor.address, value: 1000n, validAfter: now - 1n, validBefore: now + 120n, nonce: `0x${randomBytes(32).toString('hex')}` };
const typed = { domain, types, primaryType: 'TransferWithAuthorization', message };
const signature = await user.signTypedData(typed);
assert(await verifyTypedData({ ...typed, address: user.address, signature }));
for (const changed of [
  { ...typed, message: { ...message, value: 1001n } },
  { ...typed, message: { ...message, to: user.address } },
  { ...typed, domain: { ...domain, chainId: 8453 } },
  { ...typed, domain: { ...domain, verifyingContract: user.address } },
]) assert.equal(await verifyTypedData({ ...changed, address: user.address, signature }), false);
const report = { observedAt: new Date().toISOString(), endpoint, scope: 'SDK EOA signature and read-only contract checks; no external wallet or broadcast', addresses: { user: user.address, sponsor: sponsor.address }, token, checks: { signatureValid: true, changedAmountRejected: true, changedRecipientRejected: true, changedChainRejected: true, changedContractRejected: true } };
const abi = parseAbi(['function name() view returns (string)', 'function version() view returns (string)', 'function decimals() view returns (uint8)', 'function DOMAIN_SEPARATOR() view returns (bytes32)', 'function TRANSFER_WITH_AUTHORIZATION_TYPEHASH() view returns (bytes32)']);
try {
  assert.equal(Number(await rpc(endpoint, 'eth_chainId', [], { direct: true })), chainId);
  report.contract = {};
  for (const functionName of ['name', 'version', 'decimals', 'DOMAIN_SEPARATOR', 'TRANSFER_WITH_AUTHORIZATION_TYPEHASH']) {
    const data = encodeFunctionData({ abi, functionName });
    const raw = await rpc(endpoint, 'eth_call', [{ to: token, data }, 'latest'], { direct: true });
    report.contract[functionName] = decodeFunctionResult({ abi, functionName, data: raw });
  }
  assert.equal(report.contract.DOMAIN_SEPARATOR, hashDomain({ domain, types: { EIP712Domain: [{ name: 'name', type: 'string' }, { name: 'version', type: 'string' }, { name: 'chainId', type: 'uint256' }, { name: 'verifyingContract', type: 'address' }] } }));
  assert.equal(report.contract.TRANSFER_WITH_AUTHORIZATION_TYPEHASH, keccak256(stringToHex('TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)')));
  report.checks.deployedDomainAndTypeMatch = true;
} catch (error) { report.error = error.message; process.exitCode = 1; }
await writeFile(`${output}/base-result.json`, JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
