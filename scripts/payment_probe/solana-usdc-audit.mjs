// Run after the current case finalizes and before another payment changes its balances.
import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT } from './solana-live-core.mjs';
import { verifyUsdcReceipt } from './solana-usdc-core.mjs';

const response = await fetch('http://127.0.0.1:18792/state');
const state = await response.json();
assert(response.ok && state?.status === 'finalized', 'Wait for the current payment to finalize');
assert.equal(state.asset, 'USDC');
assert(['solana-usdc-new-ata', 'solana-usdc-existing-ata'].includes(state.probeCase));
verifyUsdcReceipt(state, state.receipt);
const entries = [...Object.entries(state.addresses), ['userAta', state.tokenAccounts.user], ['recipientAta', state.tokenAccounts.recipient]];
const accounts = await rpc(DEVNET_ENDPOINT, 'getMultipleAccounts', [entries.map(([, address]) => address), {
  encoding: 'jsonParsed', commitment: 'confirmed', minContextSlot: state.receipt.slot,
}]);
const observed = {};
for (const [position, [role, address]] of entries.entries()) {
  const account = accounts.value[position];
  const receiptIndex = state.receipt.transaction.message.accountKeys.indexOf(address);
  assert(receiptIndex >= 0 && account, 'Missing audited account');
  assert.equal(account.lamports, state.receipt.meta.postBalances[receiptIndex], 'SOL changed since this payment; inspect other transactions');
  observed[role] = { address, lamports: account.lamports };
  const token = state.receipt.meta.postTokenBalances.find(item => item.accountIndex === receiptIndex);
  if (token) {
    const info = account.data.parsed.info;
    assert.equal(account.owner, token.programId);
    assert.equal(info.owner, token.owner);
    assert.equal(info.mint, token.mint);
    assert.equal(info.tokenAmount.amount, token.uiTokenAmount.amount, 'USDC changed since this payment; inspect other transactions');
    observed[role].tokenAmountAtomic = info.tokenAmount.amount;
  }
}
const result = { observedAt: new Date().toISOString(), signature: state.signature, slot: accounts.context.slot, matchesSingleReceipt: true, observed };
const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
await writeFile(`${output}/${state.probeCase}-balance-audit.json`, JSON.stringify(result, null, 2));
console.log(JSON.stringify(result, null, 2));
