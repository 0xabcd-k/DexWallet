// Offline signer for one explicitly authorized Nile test payout.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { TronWeb } from 'tronweb';
import { tron, headerOptions } from './tron-live-core.mjs';

const [policyPath, preparedPath, referencePath, keyPath, output] = process.argv.slice(2);
assert(policyPath && preparedPath && referencePath && keyPath && output);
const load = async path => JSON.parse(await readFile(path, 'utf8'));
const [policy, row, reference] = await Promise.all([policyPath, preparedPath, referencePath].map(load));
assert.equal(policy.Network, 'tron-nile');
assert.equal(reference.genesis, '0000000000000000d698d4192c56cb6be724a558448e2684802de4d6cd8690dc');
assert.equal(row.Network, policy.Network);
assert.equal(row.State, 'prepared');
assert.equal(row.RequestKey, policy.Key);
assert.equal(row.Sender, policy.Sender);
assert.equal(row.Recipient, policy.Recipient);
assert.equal(row.Sun, policy.Sun);
assert.notEqual(row.Sender, row.Recipient);
assert.equal(reference.block.blockID, row.RefBlockId);
assert.equal(reference.block.block_header.raw_data.number, row.RefHeight);
assert.equal(reference.block.block_header.raw_data.timestamp, row.TimestampMs);
const options = headerOptions(reference.block);
assert.equal(options.blockHeader.expiration, row.ExpirationMs);
assert(Date.now() >= row.TimestampMs && Date.now() < row.ExpirationMs);
assert.equal(row.TokenContract ?? '', policy.TokenContract ?? '');
assert.equal(row.TokenAtoms ?? '', policy.TokenAtoms ?? '');
assert.equal(row.FeeLimitSun ?? 0, policy.FeeLimitSun ?? 0);
let tx;
if (policy.TokenContract) {
  assert.equal(row.AssetId, 'tron-nile:usdt:' + policy.TokenContract);
  assert.equal(row.Sun, 0);
  assert(typeof policy.TokenAtoms === 'string' && /^[1-9][0-9]{0,77}$/.test(policy.TokenAtoms) && BigInt(policy.TokenAtoms) < 2n ** 256n);
  assert(Number.isSafeInteger(policy.FeeLimitSun) && policy.FeeLimitSun > 0);
  ({ transaction: tx } = await tron.transactionBuilder.triggerSmartContract(
    policy.TokenContract, 'transfer(address,uint256)', { ...options, txLocal: true, feeLimit: policy.FeeLimitSun },
    [{ type: 'address', value: policy.Recipient }, { type: 'uint256', value: policy.TokenAtoms }], policy.Sender));
} else {
  assert.equal(row.AssetId ?? 'tron-nile:trx', 'tron-nile:trx');
  assert.equal(row.TokenAtoms ?? '', '');
  assert.equal(row.FeeLimitSun ?? 0, 0);
  assert(Number.isSafeInteger(row.Sun) && row.Sun > 0);
  tx = await tron.transactionBuilder.sendTrx(policy.Recipient, policy.Sun, policy.Sender, options);
}
assert.equal(tx.raw_data_hex, row.RawHex);
assert.equal(tx.txID, row.TxId);
const key = await load(keyPath);
assert.equal(TronWeb.address.fromPrivateKey(key), policy.Sender);
const signed = await tron.trx.sign(tx, key);
assert.equal(signed.signature.length, 1);
assert.equal(tron.trx.ecRecover(signed), policy.Sender);
await writeFile(output, signed.signature[0] + '\n', { mode: 0o600, flag: 'wx' });
