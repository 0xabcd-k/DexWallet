// Offline Base Sepolia fixture signer. Check the independent test policy before signing.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { keccak256, serializeTransaction } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { baseSepolia } from 'viem/chains';

const [policyFile, preparedFile, keyFile, output] = process.argv.slice(2);
assert(policyFile && preparedFile && keyFile && output, 'policy, prepared intent, test key and output path required');
const load = async (path) => JSON.parse(await readFile(path, 'utf8'));
const policy = await load(policyFile);
const row = await load(preparedFile);
const account = privateKeyToAccount(await load(keyFile));
assert.equal(policy.ChainID, baseSepolia.id);
assert.equal(row.ChainId, policy.ChainID);
assert.equal(row.State, 'prepared');
assert.equal(row.RequestKey, policy.Key);
assert.equal(account.address.toLowerCase(), row.Sender);
assert.equal(policy.Sender.toLowerCase(), row.Sender);
assert.equal(row.Recipient, policy.Recipient.toLowerCase());
for (const field of ['WeiAmount', 'FeeCap', 'TipCap', 'GasLimit', 'Nonce']) assert.equal(row[field], policy[field]);
const nonce = Number(row.Nonce);
assert(Number.isSafeInteger(nonce) && nonce >= 0);
assert(Number.isSafeInteger(row.GasLimit) && row.GasLimit >= 21000);
const tx = {
  type: 'eip1559', chainId: baseSepolia.id, nonce, to: row.Recipient,
  value: BigInt(row.WeiAmount), gas: BigInt(row.GasLimit),
  maxFeePerGas: BigInt(row.FeeCap), maxPriorityFeePerGas: BigInt(row.TipCap),
};
assert.equal(keccak256(serializeTransaction(tx)), row.SigningHash);
await writeFile(output, (await account.signTransaction(tx)) + '\n', { mode: 0o600, flag: 'wx' });
