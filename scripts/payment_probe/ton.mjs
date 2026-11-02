import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { WalletContractV5R1, beginCell, internal, SendMode } from '@ton/ton';
import { keyPairFromSeed, signVerify } from '@ton/crypto';

// Ephemeral SDK keys, no wallet account connection and no chain submission.
const user = keyPairFromSeed(randomBytes(32));
const wallet = WalletContractV5R1.create({ publicKey: user.publicKey, walletId: { networkGlobalId: -3 } });
const args = { seqno: 1, timeout: Math.floor(Date.now() / 1000) + 120, secretKey: user.secretKey, authType: 'internal', sendMode: SendMode.PAY_GAS_SEPARATELY };
const body = wallet.createTransfer({ ...args, messages: [internal({ to: wallet.address, value: 1n })] });
function unpack(cell) {
  const slice = cell.beginParse();
  const builder = beginCell().storeBits(slice.loadBits(slice.remainingBits - 512));
  const signature = slice.loadBuffer(64);
  while (slice.remainingRefs) builder.storeRef(slice.loadRef());
  return { unsigned: builder.endCell(), signature };
}
const { unsigned, signature } = unpack(body);
assert(signVerify(unsigned.hash(), signature, user.publicKey));
assert.equal(unsigned.beginParse().loadUint(32), WalletContractV5R1.OpCodes.auth_signed_internal);
const changed = wallet.createTransfer({ ...args, messages: [internal({ to: wallet.address, value: 2n })] });
assert.notDeepEqual(changed.hash(), body.hash());
assert.equal(signVerify(unpack(changed).unsigned.hash(), signature, user.publicKey), false);
const report = { observedAt: new Date().toISOString(), scope: 'Offline SDK only; no wallet app or chain execution', networkGlobalId: -3, wallet: wallet.address.toString({ testOnly: true }), checks: { signedInternalOpcode: true, localSignatureVerified: true, changedAmountRejected: true }, broadcast: false };
const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
await mkdir(output, { recursive: true, mode: 0o700 });
await writeFile(`${output}/ton-result.json`, JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
