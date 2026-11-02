import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { TronWeb } from 'tronweb';
import { post } from './rpc.mjs';

const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
await mkdir(output, { recursive: true, mode: 0o700 });
let keys;
try { keys = JSON.parse(await readFile(`${output}/tron-test-keys.json`, 'utf8')); }
catch (error) {
  if (error.code !== 'ENOENT') throw error;
  keys = { user: await TronWeb.createAccount(), sponsor: await TronWeb.createAccount() };
  await writeFile(`${output}/tron-test-keys.json`, JSON.stringify(keys), { flag: 'wx', mode: 0o600 });
}
const endpoint = 'https://nile.trongrid.io';
const tron = new TronWeb({ fullHost: endpoint });
const report = { observedAt: new Date().toISOString(), endpoint, scope: 'SDK signature and live block reference, no external wallet or broadcast', addresses: { user: keys.user.address.base58, sponsor: keys.sponsor.address.base58 } };
try {
  const block = await post(`${endpoint}/wallet/getnowblock`, {}, { direct: true });
  const raw = block.block_header.raw_data;
  report.block = { height: raw.number, id: block.blockID, timestamp: raw.timestamp };
  const unsigned = await tron.transactionBuilder.sendTrx(report.addresses.sponsor, 1000, report.addresses.user, {
    blockHeader: { ref_block_bytes: block.blockID.slice(12, 16), ref_block_hash: block.blockID.slice(16, 32), expiration: raw.timestamp + 60000, timestamp: raw.timestamp },
  });
  const signed = await tron.trx.sign(unsigned, keys.user.privateKey);
  assert.equal(tron.trx.ecRecover(signed), report.addresses.user);
  const altered = structuredClone(signed);
  altered.raw_data.contract[0].parameter.value.amount = 1001;
  assert.throws(() => tron.trx.ecRecover(altered), /Invalid transaction/);
  const changedRecipient = structuredClone(signed);
  changedRecipient.raw_data.contract[0].parameter.value.to_address = TronWeb.address.toHex(report.addresses.user);
  assert.throws(() => tron.trx.ecRecover(changedRecipient), /Invalid transaction/);
  report.checks = { signatureRecoveredUser: true, modifiedJsonAmountRejected: true, modifiedJsonRecipientRejected: true, signedTxID: signed.txID, transactionOwner: signed.raw_data.contract[0].parameter.value.owner_address, separateFeePayerPresent: false };
  report.accountResources = await post(`${endpoint}/wallet/getaccountresource`, { address: report.addresses.user, visible: true }, { direct: true });
} catch (error) { report.error = error.message; process.exitCode = 1; }
await writeFile(`${output}/tron-result.json`, JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
