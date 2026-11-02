// Dedicated testnet sponsor funding only; this is not a product bridge feature.
import assert from 'node:assert/strict';
import { readFile, writeFile, rename } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { privateKeyToAccount } from 'viem/accounts';
import { baseSepolia } from 'viem/chains';
import { encodeFunctionData, keccak256, parseAbi, toHex } from 'viem';
import { getL2TransactionHashes } from 'viem/op-stack';
import { rpc } from './rpc.mjs';
import { CHAIN_ID, ENDPOINT, json } from './base-live-core.mjs';

const mode = process.argv[2] ?? '--check';
assert(process.argv.length <= 3 && ['--send', '--check'].includes(mode));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const path = `${root}/base-funding-deposit-state.json`;
const l1 = 'https://ethereum-sepolia-rpc.publicnode.com';
const l1ChainId = 11155111;
// Base's official contract list and pinned viem chain metadata agree on this portal.
const portal = '0x49f53e41452C74589E85cA1677426Ba426459e85';
assert.equal(baseSepolia.contracts.portal[l1ChainId].address.toLowerCase(), portal.toLowerCase());
const amount = 1_000_000_000_000_000n; // Bridge 0.001 test ETH, leaving most faucet funds on L1.
const maxL1Fee = 1_000_000_000_000_000n;
const request = (url, method, params = []) => rpc(url, method, params, { direct: true });
assert.equal(Number(await request(l1, 'eth_chainId')), l1ChainId);
assert.equal(Number(await request(ENDPOINT, 'eth_chainId')), CHAIN_ID);
let state;
try { state = JSON.parse(await readFile(path, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
async function save() {
  await writeFile(path + '.tmp', json(state) + '\n', { mode: 0o600 }); await rename(path + '.tmp', path);
}
if (mode === '--send') {
  assert(!state, 'Existing funding attempt: check its saved hashes; never create a second deposit automatically');
  const sponsor = privateKeyToAccount(JSON.parse(await readFile(`${root}/base-sponsor-only.json`, 'utf8')));
  assert.notEqual(await request(l1, 'eth_getCode', [portal, 'latest']), '0x');
  const portalAbi = parseAbi(['function depositTransaction(address to,uint256 value,uint64 gasLimit,bool isCreation,bytes data) payable']);
  const data = encodeFunctionData({ abi: portalAbi, functionName: 'depositTransaction', args: [sponsor.address, amount, 100_000n, false, '0x'] });
  const call = { from: sponsor.address, to: portal, value: toHex(amount), data };
  await request(l1, 'eth_call', [call, 'latest']);
  const estimate = BigInt(await request(l1, 'eth_estimateGas', [call]));
  const block = await request(l1, 'eth_getBlockByNumber', ['latest', false]);
  const priority = BigInt(await request(l1, 'eth_maxPriorityFeePerGas'));
  const gas = estimate * 12n / 10n;
  const maxFeePerGas = BigInt(block.baseFeePerGas) * 2n + priority;
  assert(gas * maxFeePerGas <= maxL1Fee, 'Testnet funding fee exceeds probe cap');
  const l1Balance = BigInt(await request(l1, 'eth_getBalance', [sponsor.address, 'latest']));
  const l2Balance = BigInt(await request(ENDPOINT, 'eth_getBalance', [sponsor.address, 'latest']));
  assert(l1Balance >= amount + gas * maxFeePerGas);
  assert.equal(l2Balance, 0n, 'Funding already present; do not deposit again');
  const nonce = Number(await request(l1, 'eth_getTransactionCount', [sponsor.address, 'pending']));
  assert(Number.isSafeInteger(nonce));
  const signed = await sponsor.signTransaction({ chainId: l1ChainId, type: 'eip1559', to: portal, data, value: amount, nonce, gas, maxFeePerGas, maxPriorityFeePerGas: priority });
  state = { observedAt: new Date().toISOString(), status: 'signed', source: 'https://docs.base.org/base-chain/network-information/base-contracts',
    l1ChainId, l2ChainId: CHAIN_ID, portal, sponsor: sponsor.address, amountWei: amount, l1BeforeWei: l1Balance, l2BeforeWei: l2Balance,
    estimate: { gas, maxFeePerGas, maxPriorityFeePerGas: priority }, signed, l1Hash: keccak256(signed) };
  await save();
  assert.equal(await request(l1, 'eth_sendRawTransaction', [signed]), state.l1Hash);
  state.status = 'submitted'; await save();
} else {
  assert(state, 'No funding deposit was submitted');
  const receipt = await request(l1, 'eth_getTransactionReceipt', [state.l1Hash]);
  if (receipt) {
    state.l1Receipt = receipt;
    assert.equal(receipt.status, '0x1', 'L1 deposit failed');
    assert.equal(receipt.to.toLowerCase(), portal.toLowerCase());
    assert.equal(receipt.from.toLowerCase(), state.sponsor.toLowerCase());
    const logs = receipt.logs.filter(log => log.address.toLowerCase() === portal.toLowerCase()).map(log => ({ ...log, logIndex: Number(log.logIndex) }));
    const hashes = getL2TransactionHashes({ logs });
    assert.equal(hashes.length, 1); state.l2Hash = hashes[0];
    state.l1FeeWei = (BigInt(receipt.gasUsed) * BigInt(receipt.effectiveGasPrice)).toString();
    state.status = 'l1-confirmed';
    const l2Receipt = await request(ENDPOINT, 'eth_getTransactionReceipt', [state.l2Hash]);
    if (l2Receipt) {
      state.l2Receipt = l2Receipt;
      assert.equal(l2Receipt.status, '0x1', 'L2 deposit failed');
      assert.equal(l2Receipt.to.toLowerCase(), state.sponsor.toLowerCase());
      assert.equal(l2Receipt.from.toLowerCase(), state.sponsor.toLowerCase());
      state.l2AfterWei = BigInt(await request(ENDPOINT, 'eth_getBalance', [state.sponsor, l2Receipt.blockNumber])).toString();
      assert.equal(BigInt(state.l2AfterWei) - BigInt(state.l2BeforeWei), BigInt(state.amountWei));
      state.status = 'funded';
    }
    await save();
  }
}
const { signed, ...publicState } = state;
await writeFile(`${root}/base-funding-deposit-public.json`, json(publicState) + '\n');
console.log(json({ status: state.status, l1Hash: state.l1Hash, l2Hash: state.l2Hash, l2AfterWei: state.l2AfterWei, l1FeeWei: state.l1FeeWei }));
