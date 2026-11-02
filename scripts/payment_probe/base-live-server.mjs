import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, writeFile, rename } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { privateKeyToAccount } from 'viem/accounts';
import { decodeFunctionResult, encodeFunctionData, hashDomain, keccak256, serializeTransaction } from 'viem';
import { rpc } from './rpc.mjs';
import { CHAIN_ID, ENDPOINT, TOKEN, ORACLE, AMOUNT, MAX_FEE_WEI, abi, oracleAbi, domain, domainTypes, json, prepareOrder, validateAuthorization, verifyReceipt, verifyBalances } from './base-live-core.mjs';

const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
// The serving process never reads the user's key. That key is imported into the original wallet UI separately.
const sponsor = privateKeyToAccount(JSON.parse(await readFile(`${root}/base-sponsor-only.json`, 'utf8')));
const addresses = JSON.parse(await readFile(`${root}/base-live-addresses.json`, 'utf8'));
assert.equal(addresses.sponsor, sponsor.address);
const origin = 'http://127.0.0.1:18793';
const statePath = `${root}/base-live-state.json`;
const request = (method, params = []) => rpc(ENDPOINT, method, params, { direct: true });
let state = null, busy = false;
try { state = JSON.parse(await readFile(statePath, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const save = async () => {
  await writeFile(`${statePath}.tmp`, json(state) + '\n', { mode: 0o600 });
  await rename(`${statePath}.tmp`, statePath);
};
const visible = () => {
  if (!state) return null;
  const { signature, signedTransaction, callData, ...result } = state;
  return result;
};
async function read(functionName, args = [], block = 'latest', oracle = false) {
  const contractAbi = oracle ? oracleAbi : abi;
  const data = encodeFunctionData({ abi: contractAbi, functionName, args });
  const result = await request('eth_call', [{ to: oracle ? ORACLE : TOKEN, data }, block]);
  return decodeFunctionResult({ abi: contractAbi, functionName, data: result });
}
async function balances(block) {
  const entries = await Promise.all(['user', 'sponsor'].map(async role => [role, {
    ethWei: BigInt(await request('eth_getBalance', [addresses[role], block])).toString(),
    usdcAtomic: (await read('balanceOf', [addresses[role]], block)).toString(),
  }]));
  return Object.fromEntries(entries);
}
async function inspect() {
  assert.equal(Number(await request('eth_chainId')), CHAIN_ID);
  const block = await request('eth_getBlockByNumber', ['latest', false]);
  const [name, version, decimals, separator, snapshot] = await Promise.all([
    read('name', [], block.number), read('version', [], block.number), read('decimals', [], block.number),
    read('DOMAIN_SEPARATOR', [], block.number), balances(block.number),
  ]);
  assert.equal(name, domain.name); assert.equal(version, domain.version); assert.equal(decimals, 6);
  assert.equal(separator, hashDomain({ domain, types: { EIP712Domain: domainTypes } }));
  return { block, balances: snapshot, contract: { name, version, decimals, separator } };
}
async function refresh() {
  if (!state?.txHash) return;
  const receipt = await request('eth_getTransactionReceipt', [state.txHash]);
  if (!receipt) return;
  state.receipt = receipt;
  if (receipt.status !== '0x1') { state.status = 'failed'; await save(); throw new Error('Onchain execution failed'); }
  const transaction = await request('eth_getTransactionByHash', [state.txHash]);
  const fees = verifyReceipt(state, receipt, transaction);
  // Query the oracle at the receipt block; fees are network-specific, not just gasUsed * price.
  fees.operatorFeeWei = (await read('getOperatorFee', [BigInt(receipt.gasUsed)], receipt.blockNumber, true)).toString();
  assert(await read('authorizationState', [addresses.user, state.typed.message.nonce], receipt.blockNumber));
  const after = await balances(receipt.blockNumber);
  state.receiptCheck = { ...fees, ...verifyBalances(state.before.balances, after, fees) };
  state.after = { blockNumber: receipt.blockNumber, balances: after };
  state.transaction = transaction;
  const finalized = await request('eth_getBlockByNumber', ['finalized', false]);
  state.finalizedBlock = finalized.number;
  state.status = BigInt(finalized.number) >= BigInt(receipt.blockNumber) ? 'finalized' : 'confirmed';
  await save();
}
async function action(path, body) {
  if (path !== '/prepare') assert(state, 'No prepared order');
  if (path === '/prepare') {
    assert.equal(body.user, addresses.user, 'Wrong test account');
    assert(!state?.signature && !state?.txHash, 'Inspect the existing signed authorization before preparing another');
    const snapshot = await inspect();
    assert.equal(BigInt(snapshot.balances.user.ethWei), 0n, 'This probe requires zero user ETH');
    state = { ...prepareOrder(addresses, snapshot.block.timestamp), preparation: snapshot };
    await save();
  } else if (path === '/validate') {
    assert(state && !state.signature && !state.txHash, 'No unsigned order');
    const block = await request('eth_getBlockByNumber', ['latest', false]);
    const callData = await validateAuthorization(state, body.reference, body.signature, block.timestamp);
    state.signature = body.signature; state.callData = callData;
    state.status = 'validated'; state.walletSignedAt = new Date().toISOString();
    await save();
  } else if (path === '/submit') {
    assert.equal(body.reference, state?.reference, 'Order reference mismatch');
    if (state.txHash) { await refresh(); return { ...visible(), reusedSubmission: true }; }
    assert.equal(state.status, 'validated', 'Wallet authorization required');
    const snapshot = await inspect();
    const data = await validateAuthorization(state, body.reference, state.signature, snapshot.block.timestamp);
    assert.equal(await read('authorizationState', [addresses.user, state.typed.message.nonce]), false, 'Authorization already used or cancelled');
    assert(BigInt(snapshot.balances.user.usdcAtomic) >= AMOUNT, 'Insufficient test USDC');
    assert.equal(BigInt(snapshot.balances.user.ethWei), 0n, 'This probe requires zero user ETH');
    const call = { from: sponsor.address, to: TOKEN, data, value: '0x0' };
    // Simulate the exact stored authorization before signing or broadcasting a platform transaction.
    const simulation = await request('eth_call', [call, 'latest']);
    const estimatedGas = BigInt(await request('eth_estimateGas', [call]));
    const gas = (estimatedGas * 12n + 9n) / 10n;
    const priority = BigInt(await request('eth_maxPriorityFeePerGas'));
    const maxFeePerGas = BigInt(snapshot.block.baseFeePerGas) * 2n + priority;
    const nonce = Number(await request('eth_getTransactionCount', [sponsor.address, 'pending']));
    assert(Number.isSafeInteger(nonce));
    const tx = { chainId: CHAIN_ID, type: 'eip1559', to: TOKEN, data, value: 0n, nonce, gas, maxFeePerGas, maxPriorityFeePerGas: priority };
    // Conservative size includes signature RLP overhead; estimates can change before inclusion.
    const sizeBound = BigInt((serializeTransaction(tx).length - 2) / 2 + 100);
    const [l1Bound, operatorBound] = await Promise.all([
      read('getL1FeeUpperBound', [sizeBound], 'latest', true), read('getOperatorFee', [gas], 'latest', true),
    ]);
    const estimatedBudget = gas * maxFeePerGas + (l1Bound + operatorBound) * 2n;
    assert(estimatedBudget <= MAX_FEE_WEI, 'Estimated fee exceeds probe budget');
    assert(BigInt(snapshot.balances.sponsor.ethWei) >= estimatedBudget, 'Insufficient platform test ETH');
    const signed = await sponsor.signTransaction(tx);
    state.before = snapshot; state.simulation = simulation;
    state.feeEstimate = { estimatedGas, gasLimit: gas, maxFeePerGas, maxPriorityFeePerGas: priority, l1UpperBoundWei: l1Bound, operatorUpperBoundWei: operatorBound, budgetWei: estimatedBudget, sizeBoundBytes: sizeBound };
    state.callData = data; state.signedTransaction = signed; state.txHash = keccak256(signed);
    state.status = 'signed';
    // Persist exact bytes and hash before broadcasting. Unknown RPC outcomes never create a new nonce.
    await save();
    assert.equal(await request('eth_sendRawTransaction', [signed]), state.txHash);
    state.status = 'submitted'; state.submittedAt = new Date().toISOString(); await save();
  } else if (path === '/replay') {
    assert.equal(body.reference, state?.reference); assert(state?.signedTransaction);
    try { state.replay = { acceptedHash: await request('eth_sendRawTransaction', [state.signedTransaction]) }; }
    catch (error) { state.replay = { error: error.message }; }
    await save(); await refresh();
  } else if (path === '/audit') {
    assert.equal(body.reference, state?.reference); await refresh();
    assert(['confirmed', 'finalized'].includes(state.status), 'Receipt required');
    const block = await request('eth_getBlockByNumber', ['latest', false]);
    const current = await balances(block.number);
    assert.deepEqual(current, state.after.balances, 'Balances changed since the receipt');
    let rejected = false, errorMessage;
    // At the receipt block the authorization was still within its validity window,
    // so the rejection proves consumed-nonce protection even if we audit after expiry.
    try { await request('eth_call', [{ from: sponsor.address, to: TOKEN, data: state.callData }, state.receipt.blockNumber]); }
    catch (error) {
      errorMessage = error.message;
      // Transport failures cannot prove nonce protection.
      rejected = /execution reverted/i.test(errorMessage) && /authorization is used or canceled/i.test(errorMessage);
    }
    assert(rejected, `Used authorization was not demonstrably rejected: ${errorMessage ?? 'call succeeded'}`);
    state.audit = { observedAt: new Date().toISOString(), block: block.number, balances: current, usedAuthorizationSimulationBlock: state.receipt.blockNumber, usedAuthorizationRejected: true, rejection: errorMessage };
    await save();
  } else throw new Error('Unknown action');
  return visible();
}
const server = createServer(async (req, res) => {
  try {
    assert.equal(req.headers.host, '127.0.0.1:18793', 'Unexpected request host');
    if (req.headers.origin) assert.equal(req.headers.origin, origin, 'Unexpected request origin');
    const path = new URL(req.url, origin).pathname;
    if (req.method === 'GET' && path === '/') {
      res.setHeader('Content-Type', 'text/html'); res.end(await readFile(new URL('./base-live.html', import.meta.url))); return;
    }
    res.setHeader('Content-Type', 'application/json');
    if (req.method === 'GET' && path === '/config') { res.end(json({ addresses, chainId: CHAIN_ID, token: TOKEN })); return; }
    assert(!busy, 'Another probe action is in progress'); busy = true;
    try {
      if (req.method === 'GET' && path === '/state') { await refresh(); res.end(json(visible())); return; }
      if (req.method !== 'POST' || !['/prepare', '/validate', '/submit', '/replay', '/audit'].includes(path)) { res.writeHead(404).end(); return; }
      assert(req.headers['content-type']?.startsWith('application/json'), 'JSON required');
      let raw = '';
      for await (const chunk of req) { raw += chunk; assert(raw.length <= 8192, 'Body too large'); }
      res.end(json(await action(path, JSON.parse(raw))));
    } finally { busy = false; }
  } catch (error) { res.writeHead(400, { 'Content-Type': 'application/json' }); res.end(json({ error: error.message })); }
});
server.listen(18793, '127.0.0.1', () => console.log(`Base Sepolia USDC probe: ${origin}; sponsor ${sponsor.address}`));
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => server.close(() => process.exit(0)));
