// Testnet only: validates a REAL wallet grant, funds principal, then sponsors execution.
// Never reads or signs with the user's private key. No automatic new-nonce retries.
import assert from 'node:assert/strict';
import { open, readFile, writeFile, rename, unlink } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { privateKeyToAccount } from 'viem/accounts';
import { decodeErrorResult, decodeEventLog, decodeFunctionResult, encodeAbiParameters, encodeFunctionData, keccak256, serializeTransaction, toHex } from 'viem';
import { rpc } from './rpc.mjs';
import { ENDPOINT, ORACLE, MAX_FEE_WEI, oracleAbi, json } from './base-live-core.mjs';
import { ACCOUNT_CODE, CHAIN_ID, ETH_AMOUNT, MANAGER, delegationParameters, managerAbi, paymentData, sameAddress, validateGrant } from './base-eth-core.mjs';

const mode = process.argv[2];
assert(process.argv.length === 3 && ['--prepare', '--fund', '--submit', '--audit'].includes(mode));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const path = `${root}/base-eth-live-state.json`;
const query = (method, params = []) => rpc(ENDPOINT, method, params, { direct: true });
const user = '0xecd14F7586f5053CECae148Ae26309C2277D86A8';
const platform = '0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA';
let state;
const save = async () => {
  await writeFile(`${path}.tmp`, json(state) + '\n', { mode: 0o600 });
  await rename(`${path}.tmp`, path);
};
async function oracle(name, args, block = 'latest') {
  const data = encodeFunctionData({ abi: oracleAbi, functionName: name, args });
  return decodeFunctionResult({ abi: oracleAbi, functionName: name, data: await query('eth_call', [{ to: ORACLE, data }, block]) });
}
async function snapshot(block = 'latest') {
  const header = await query('eth_getBlockByNumber', [block, false]);
  const [userBalance, platformBalance, userCode, userNonce, platformNonce] = await Promise.all([
    query('eth_getBalance', [user, header.number]), query('eth_getBalance', [platform, header.number]),
    query('eth_getCode', [user, header.number]), query('eth_getTransactionCount', [user, header.number]),
    query('eth_getTransactionCount', [platform, header.number]),
  ]);
  return { block: header.number, timestamp: header.timestamp, baseFeePerGas: header.baseFeePerGas,
    userWei: BigInt(userBalance).toString(), platformWei: BigInt(platformBalance).toString(), userCode, userNonce, platformNonce };
}
async function rejectedCall(call, block, pattern) {
  let rejection;
  try { await query('eth_call', [call, block]); }
  catch (error) {
    rejection = error.message;
    // Preserve RPC details and decode the manager's custom error when no reason string exists.
    try {
      const payload = JSON.parse(rejection);
      const decoded = decodeErrorResult({ abi: managerAbi, data: payload.data });
      rejection += `; ${decoded.errorName}`;
    } catch { /* A transport error still fails the contract-rejection assertion below. */ }
  }
  assert(rejection && /execution reverted/i.test(rejection) && pattern.test(rejection), `Expected contract rejection: ${rejection ?? 'call succeeded'}`);
  return rejection;
}
async function refresh(kind) {
  const entry = state[kind];
  if (!entry) return;
  const receipt = await query('eth_getTransactionReceipt', [entry.hash]);
  if (!receipt) return;
  entry.receipt = receipt;
  if (receipt.status !== '0x1') { entry.status = 'failed'; await save(); throw new Error(`${kind} transaction reverted`); }
  const transaction = await query('eth_getTransactionByHash', [entry.hash]);
  assert.equal(transaction.hash, entry.hash);
  assert.equal(Number(transaction.chainId), CHAIN_ID);
  assert(sameAddress(transaction.from, platform) && sameAddress(receipt.from, platform));
  assert(sameAddress(transaction.to, entry.to) && sameAddress(receipt.to, entry.to));
  assert.equal(BigInt(transaction.value), BigInt(entry.value));
  assert.equal(transaction.input, entry.data);
  const before = await snapshot(toHex(BigInt(receipt.blockNumber) - 1n));
  const after = await snapshot(receipt.blockNumber);
  // Dedicated accounts must have had no other transfers between preparation and inclusion.
  for (const key of ['userWei', 'platformWei', 'userNonce', 'platformNonce']) assert.equal(before[key], entry.before[key]);
  const fees = { l2Wei: (BigInt(receipt.gasUsed) * BigInt(receipt.effectiveGasPrice)).toString(),
    l1Wei: BigInt(receipt.l1Fee).toString(), operatorWei: (await oracle('getOperatorFee', [BigInt(receipt.gasUsed)], receipt.blockNumber)).toString() };
  fees.totalWei = (BigInt(fees.l2Wei) + BigInt(fees.l1Wei) + BigInt(fees.operatorWei)).toString();
  assert(BigInt(fees.totalWei) > 0n && BigInt(fees.totalWei) <= MAX_FEE_WEI);
  assert.equal(BigInt(after.platformNonce), BigInt(before.platformNonce) + 1n);
  assert.equal(after.userNonce, before.userNonce, 'User sent an outer transaction');
  assert.equal(after.userCode, ACCOUNT_CODE);
  if (kind === 'funding') {
    assert.equal(BigInt(before.userWei), 0n); assert.equal(BigInt(after.userWei), ETH_AMOUNT);
    assert.equal(BigInt(before.platformWei) - BigInt(after.platformWei), ETH_AMOUNT + BigInt(fees.totalWei));
  } else {
    assert.equal(BigInt(before.userWei), ETH_AMOUNT); assert.equal(BigInt(after.userWei), 0n);
    assert.equal(BigInt(after.platformWei) - BigInt(before.platformWei), ETH_AMOUNT - BigInt(fees.totalWei));
    const events = receipt.logs.filter(log => sameAddress(log.address, MANAGER)).flatMap(log => {
      try { return [decodeEventLog({ abi: managerAbi, ...log, strict: true })]; } catch { return []; }
    }).filter(event => event.eventName === 'RedeemedDelegation');
    assert.equal(events.length, 1);
    assert(sameAddress(events[0].args.rootDelegator, user) && sameAddress(events[0].args.redeemer, platform));
    assert.equal(encodeAbiParameters(delegationParameters, [[events[0].args.delegation]]), state.grant.context);
  }
  entry.after = after; entry.fees = fees; entry.transaction = transaction;
  const finalized = await query('eth_getBlockByNumber', ['finalized', false]);
  entry.finalizedBlock = finalized.number;
  entry.status = BigInt(finalized.number) >= BigInt(receipt.blockNumber) ? 'finalized' : 'confirmed';
  await save();
}
async function submit(kind) {
  if (state[kind]) {
    await refresh(kind);
    // Only resend the exact persisted bytes after an uncertain initial RPC result.
    if (state[kind].status === 'signed') {
      const known = await query('eth_getTransactionByHash', [state[kind].hash]);
      if (!known) {
        const current = await snapshot();
        await validateGrant(state.grant, state.intent, current.timestamp);
        const entry = state[kind];
        await query('eth_call', [{ from: platform, to: entry.to, data: entry.data, value: toHex(BigInt(entry.value)) }, current.block]);
        assert.equal(await query('eth_sendRawTransaction', [entry.raw]), entry.hash);
      }
      state[kind].status = 'submitted'; await save();
    }
    return;
  }
  const before = await snapshot();
  assert.equal(before.userCode, ACCOUNT_CODE);
  const authorization = await validateGrant(state.grant, state.intent, before.timestamp);
  let to, data, value;
  if (kind === 'funding') {
    assert.equal(BigInt(before.userWei), 0n, 'Do not add principal twice');
    to = user; data = '0x'; value = ETH_AMOUNT;
  } else {
    await refresh('funding'); assert(['confirmed', 'finalized'].includes(state.funding?.status), 'Principal funding must be confirmed');
    assert.equal(BigInt(before.userWei), ETH_AMOUNT, 'User must hold principal only');
    to = MANAGER; data = authorization.data; value = 0n;
    const call = { from: platform, to, data, value: '0x0' };
    state.simulation = await query('eth_call', [call, before.block]);
    state.negativeSimulations = {
      overAllowance: await rejectedCall({ ...call, data: paymentData(state.grant.context, platform, ETH_AMOUNT + 1n) }, before.block, /NativeTokenPeriodTransferEnforcer/),
      wrongPayee: await rejectedCall({ ...call, data: paymentData(state.grant.context, user) }, before.block, /AllowedTargetsEnforcer/),
      wrongRedeemer: await rejectedCall({ ...call, from: user }, before.block, /InvalidDelegate|RedeemerEnforcer/),
      unexpectedCalldata: await rejectedCall({ ...call, data: paymentData(state.grant.context, platform, ETH_AMOUNT, '0x01') }, before.block, /ExactCalldataEnforcer/),
    };
    await save();
  }
  const call = { from: platform, to, data, value: toHex(value) };
  assert.equal(await query('eth_call', [call, 'latest']), '0x');
  const estimatedGas = BigInt(await query('eth_estimateGas', [call]));
  const gas = (estimatedGas * 12n + 9n) / 10n;
  const priority = BigInt(await query('eth_maxPriorityFeePerGas'));
  const maxFeePerGas = BigInt(before.baseFeePerGas) * 2n + priority;
  const pendingNonce = await query('eth_getTransactionCount', [platform, 'pending']);
  assert.equal(BigInt(pendingNonce), BigInt(before.platformNonce), 'Another platform transaction is pending');
  const nonce = Number(pendingNonce); assert(Number.isSafeInteger(nonce));
  const transaction = { chainId: CHAIN_ID, type: 'eip1559', to, data, value, nonce, gas, maxFeePerGas, maxPriorityFeePerGas: priority };
  const size = BigInt((serializeTransaction(transaction).length - 2) / 2 + 100);
  const [l1, operator] = await Promise.all([oracle('getL1FeeUpperBound', [size]), oracle('getOperatorFee', [gas])]);
  const budget = gas * maxFeePerGas + (l1 + operator) * 2n;
  assert(budget <= MAX_FEE_WEI && BigInt(before.platformWei) >= budget + value, 'Platform fee budget unavailable');
  const sponsor = privateKeyToAccount(JSON.parse(await readFile(`${root}/base-sponsor-only.json`, 'utf8')));
  assert(sameAddress(sponsor.address, platform));
  const raw = await sponsor.signTransaction(transaction);
  state[kind] = { status: 'signed', to, data, value, raw, hash: keccak256(raw), before,
    feeEstimate: { estimatedGas, gasLimit: gas, maxFeePerGas, priority, sizeBytes: size, l1UpperWei: l1, operatorWei: operator, budgetWei: budget } };
  await save(); // Persist before any broadcast. A repeated CLI run cannot generate another transfer.
  assert.equal(await query('eth_sendRawTransaction', [raw]), state[kind].hash);
  state[kind].status = 'submitted'; state[kind].submittedAt = new Date().toISOString(); await save();
}

const lock = await open(`${path}.lock`, 'wx', 0o600);
try {
  assert.equal(Number(await query('eth_chainId')), CHAIN_ID);
  try { state = JSON.parse(await readFile(path, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  if (mode === '--prepare') {
    assert(!state, 'Review existing ETH state; do not overwrite');
    const response = JSON.parse(await readFile(`${root}/base-eth-13.53-private-grant.json`, 'utf8'));
    const { request } = JSON.parse(await readFile(`${root}/base-eth-13.53-request.json`, 'utf8'));
    assert.equal(response.ok, true); assert.equal(response.value.length, 1);
    const intent = { user, sponsor: platform, start: request.permission.data.startTime, expiry: request.rules.find(rule => rule.type === 'expiry').data.timestamp };
    const before = await snapshot();
    const result = await validateGrant(response.value[0], intent, before.timestamp);
    state = { preparedAt: new Date().toISOString(), walletVersion: '13.53.0.0', intent, grant: response.value[0], validation: result, initial: before };
    await save();
  } else {
    assert(state, 'Prepare the wallet grant first');
    assert.equal(state.intent.user, user); assert.equal(state.intent.sponsor, platform);
    if (mode === '--fund') await submit('funding');
    if (mode === '--submit') await submit('payment');
    if (mode === '--audit') {
      await refresh('funding'); await refresh('payment');
      if (['confirmed', 'finalized'].includes(state.payment?.status)) {
        state.exhaustedAllowanceRejection = await rejectedCall({ from: platform, to: MANAGER, data: state.validation.data, value: '0x0' }, state.payment.receipt.blockNumber, /NativeTokenPeriodTransferEnforcer/);
        const omitPrivate = entry => { const { raw, ...publicEntry } = entry; return publicEntry; };
        await writeFile(`${root}/base-eth-live-public-result.json`, json({
          observedAt: new Date().toISOString(), chainId: CHAIN_ID, walletVersion: state.walletVersion,
          intent: state.intent, amountWei: ETH_AMOUNT, signatureVerified: true, typedDataHash: state.validation.typedDataHash,
          simulation: state.simulation, negativeSimulations: state.negativeSimulations,
          exhaustedAllowanceRejection: state.exhaustedAllowanceRejection,
          funding: omitPrivate(state.funding), payment: omitPrivate(state.payment),
        }) + '\n');
        await save();
      }
    }
  }
  console.log(json({ mode, signatureVerified: state.validation.signatureVerified,
    funding: state.funding && { status: state.funding.status, hash: state.funding.hash, fees: state.funding.fees },
    payment: state.payment && { status: state.payment.status, hash: state.payment.hash, fees: state.payment.fees } }));
} finally { await lock.close(); await unlink(`${path}.lock`); }
