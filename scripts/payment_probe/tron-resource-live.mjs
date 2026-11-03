// Same-code Nile test token: real TronLink signing, both resources delegated by platform.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { open, readFile, writeFile, rename, unlink } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { utils } from 'tronweb';
import { post, rpc } from './rpc.mjs';
import { ENDPOINT, CHAIN_ID, tron, hex, headerOptions, RESULT_BANDWIDTH, SETUP_MEMO } from './tron-live-core.mjs';
import { USDT, AMOUNT_UNITS, wordAddress, transferData, buildTransfer, checkTransfer, verifySignature, simulationEnergy } from './tron-usdt-core.mjs';
import { FIXTURE_NAME, FIXTURE_SYMBOL, FIXTURE_SUPPLY, USER_FUND_UNITS, DEPLOY_FEE_LIMIT, REFERENCE_RUNTIME_HASH,
  ResourceType, codeHash, fixtureCreation, resourcePlan, unusedSponsorBandwidthStake, requirePlatformResources, verifyResourcePayment } from './tron-resource-core.mjs';

const mode = process.argv[2] ?? '--inspect';
assert(process.argv.length <= 3 && ['--inspect', '--setup', '--prepare', '--submit', '--audit', '--replay'].includes(mode));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const path = `${root}/tron-resource-state.json`;
const addresses = JSON.parse(await readFile(`${root}/tron-live-addresses.json`, 'utf8'));
assert.deepEqual(addresses, { user: 'TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio', sponsor: 'TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH' });
let state;
const query = async (method, body = {}, solid = false) => {
  // This experiment uses the host proxy after direct Nile requests timed out.
  const url = `${ENDPOINT}/${solid ? 'walletsolidity' : 'wallet'}/${method}`;
  let r;
  try { r = await post(url, body); }
  catch (error) {
    // Observed intermittent proxy TLS timeouts. Only read-only calls may retry
    // once on the direct route; ambiguous broadcasts must resume by saved txID.
    if (!method.startsWith('broadcast') && /curl (28|35|52|56):/.test(error.message)) {
      console.error(`Nile ${method}: proxy transport failed; retrying read once directly`);
      r = await post(url, body, { direct: true });
    } else throw new Error(`Nile ${solid ? 'walletsolidity' : 'wallet'}/${method}: ${error.message}`, { cause: error });
  }
  assert(!r.Error, `RPC ${method}: ${r.Error}`); return r;
};
const save = async () => { await writeFile(path + '.tmp', JSON.stringify(state, null, 2), { mode: 0o600 }); await rename(path + '.tmp', path); };
async function constant(selector, parameter = '', owner = addresses.user) {
  const r = await query('triggerconstantcontract', { owner_address: hex(owner), contract_address: hex(state.token), function_selector: selector, parameter });
  assert.equal(r.result?.result, true);
  assert(!r.transaction?.ret?.some(x => x.ret === 'FAILED' || (x.contractRet && x.contractRet !== 'SUCCESS')));
  assert.equal(r.constant_result?.length, 1); return r.constant_result[0];
}
async function snapshot() {
  const s = { observedAt: new Date().toISOString(), addresses, maxDelegatable: {} };
  for (const [role, address] of Object.entries(addresses)) s[role] = {
    account: await query('getaccount', { address, visible: true }),
    resources: await query('getaccountresource', { address, visible: true }),
    ...(state.token ? { tokenUnits: BigInt(`0x${await constant('balanceOf(address)', wordAddress(address))}`).toString() } : {}),
  };
  s.delegation = await query('getdelegatedresourcev2', { fromAddress: hex(addresses.sponsor), toAddress: hex(addresses.user) });
  for (const [name, type] of Object.entries(ResourceType)) s.maxDelegatable[name] =
    (await query('getcandelegatedmaxsize', { owner_address: hex(addresses.sponsor), type })).max_size ?? 0;
  if (state.token) {
    const c = await query('getcontract', { value: state.token, visible: true });
    s.contract = { address: c.contract_address, origin: c.origin_address,
      consume_user_resource_percent: c.consume_user_resource_percent ?? 0, origin_energy_limit: c.origin_energy_limit };
  }
  s.completedAt = new Date().toISOString();
  return s;
}
async function checkFixture() {
  assert(state.token && state.token !== USDT, 'Fixture must have a distinct token identity');
  const c = await query('getcontract', { value: state.token, visible: true });
  const info = await query('getcontractinfo', { value: state.token, visible: true });
  assert.equal(codeHash(info.runtimecode), REFERENCE_RUNTIME_HASH);
  assert.equal(c.origin_address, addresses.sponsor); assert.equal(c.consume_user_resource_percent, 100);
  assert.equal(BigInt(`0x${await constant('decimals()')}`), 6n);
  assert.equal(tron.utils.abi.decodeParams([], ['string'], `0x${await constant('symbol()')}`)[0], FIXTURE_SYMBOL);
  assert.equal(hex(tron.utils.abi.decodeParams([], ['address'], `0x${await constant('owner()')}`)[0]), hex(addresses.sponsor));
  state.fixtureIdentity = { observedAt: new Date().toISOString(), token: state.token, runtimeHash: REFERENCE_RUNTIME_HASH,
    symbol: FIXTURE_SYMBOL, decimals: 6, owner: addresses.sponsor, userFeePercentage: 100, tetherIssued: false };
}
async function simulate() {
  const raw = await query('triggerconstantcontract', { owner_address: hex(addresses.user), contract_address: hex(state.token), data: transferData(addresses.sponsor) });
  return { raw, energy: simulationEnergy(raw, addresses, state.token) };
}
function bandwidthOf(tx) {
  const pb = utils.transaction.txJsonToPb(tx); pb.addSignature(Buffer.alloc(65));
  return pb.serializeBinary().length + RESULT_BANDWIDTH;
}
async function confirmation(entry) {
  const info = await query('gettransactioninfobyid', { value: entry.signed.txID }, true);
  if (!info.id) return false;
  const tx = await query('gettransactionbyid', { value: entry.signed.txID }, true);
  entry.info = info; entry.transaction = tx; entry.status = 'observed'; await save();
  assert.equal(tx.raw_data_hex, entry.signed.raw_data_hex);
  assert.deepEqual(tx.ret.map(x => x.contractRet), ['SUCCESS']); assert.notEqual(info.result, 'FAILED');
  entry.status = 'solidified'; return true;
}
async function submit(entry) {
  if (!(await query('gettransactionbyid', { value: entry.signed.txID })).txID) {
    assert(Date.now() < entry.signed.raw_data.expiration, 'Saved transaction expired; inspect before replacement');
    if (entry.signed.raw_data.contract[0].type === 'CreateSmartContract') {
      // The large deployment JSON timed out on the direct Nile route. Submit the
      // exact same signed protobuf through the host proxy, never a replacement.
      const pb = utils.transaction.txJsonToPb(entry.signed);
      for (const signature of entry.signed.signature) pb.addSignature(Buffer.from(signature, 'hex'));
      entry.broadcast = await post(`${ENDPOINT}/wallet/broadcasthex`, { transaction: Buffer.from(pb.serializeBinary()).toString('hex') });
    } else entry.broadcast = await query('broadcasttransaction', entry.signed);
    await save();
    assert(entry.broadcast.result === true || entry.broadcast.code === 'DUP_TRANSACTION_ERROR', JSON.stringify(entry.broadcast));
  }
  for (let attempt = 0; attempt < 35; attempt++) {
    if (await confirmation(entry)) { await save(); return; }
    await new Promise(resolve => setTimeout(resolve, 2000));
  }
  throw new Error('Pending solidity confirmation; resume the saved txID');
}
async function setupStep(name, build, maxFee, preserveBandwidth = false) {
  if (state.setup[name]?.status === 'solidified') return;
  if (!state.setup[name]) {
    const before = await snapshot();
    let unsigned = await build(headerOptions(await query('getnowblock')));
    // Keep the platform's delegatable Bandwidth intact during resource preparation.
    if (preserveBandwidth) {
      unsigned = await tron.transactionBuilder.addUpdateData(unsigned, SETUP_MEMO, 'utf8', { txLocal: true });
      const r = before.sponsor.resources, bandwidth = bandwidthOf(unsigned);
      assert(bandwidth > (r.NetLimit ?? 0) - (r.NetUsed ?? 0) && bandwidth > (r.freeNetLimit ?? 0) - (r.freeNetUsed ?? 0), 'Setup would consume resources intended for delegation');
    }
    assert.equal(hex(unsigned.raw_data.contract[0].parameter.value.owner_address), hex(addresses.sponsor));
    const key = JSON.parse(await readFile(`${root}/tron-sponsor-only.json`, 'utf8'));
    const signed = await tron.trx.sign(unsigned, key);
    assert.equal(tron.trx.ecRecover(signed), addresses.sponsor);
    state.setup[name] = { signed, before, status: 'signed', maxFeeSun: maxFee }; await save();
  }
  const entry = state.setup[name]; await submit(entry);
  assert((entry.info.fee ?? 0) <= entry.maxFeeSun, 'Setup fee exceeded explicit test bound');
  entry.after = await snapshot(); await save();
  console.log(JSON.stringify({ step: name, txID: entry.signed.txID, feeSun: entry.info.fee ?? 0 }));
}
async function audit() {
  assert(state.payment && await confirmation(state.payment), 'Payment not solidified');
  const p = state.payment;
  checkTransfer(p.transaction, addresses, state.token);
  assert.deepEqual(p.transaction.signature.map(v => Buffer.from(v, 'hex')), p.signed.signature.map(v => Buffer.from(v, 'hex')));
  p.after ??= await snapshot();
  await save(); // Preserve the observed snapshot even when a reconciliation check fails.
  p.checks = verifyResourcePayment(p.info, p.before, p.after, state.token, p.simulation.energy, p.signature.bandwidthEstimate);
  await save();
  await writeFile(`${root}/tron-resource-public-result.json`, JSON.stringify({ observedAt: new Date().toISOString(), network: 'Nile', chainId: CHAIN_ID,
    addresses, fixtureIdentity: state.fixtureIdentity, resourcePlan: state.resourcePlan, setup: state.setup,
    orderReference: state.order.reference, payment: p, replay: state.replay, scope: 'Same-code test token; not Tether-issued USDT' }, null, 2));
}
const lock = await open(path + '.lock', 'wx', 0o600);
try {
  assert.equal(await rpc(ENDPOINT + '/jsonrpc', 'eth_chainId', []), CHAIN_ID);
  try { state = JSON.parse(await readFile(path, 'utf8')); } catch (e) { if (e.code !== 'ENOENT') throw e; }
  state ??= { addresses, setup: {}, createdAt: new Date().toISOString() };
  assert.deepEqual(state.addresses, addresses);
  if (mode === '--inspect') {
    const before = await snapshot();
    const reference = { observedAt: new Date().toISOString(), token: USDT,
      contract: await query('getcontract', { value: USDT, visible: true }), contractInfo: await query('getcontractinfo', { value: USDT, visible: true }) };
    const creation = fixtureCreation(reference);
    const quote = resourcePlan(before, 14_650, 345); // Reference estimate only; re-simulate the actual clone before staking.
    const result = { observedAt: new Date().toISOString(), before, quote, referenceRuntimeHash: codeHash(reference.contractInfo.runtimecode),
      cloneCreationBytes: creation.bytecode.length / 2, originalConstructorBytes: creation.originalConstructorBytes,
      deployFeeLimitSun: DEPLOY_FEE_LIMIT, estimateOnly: true, broadcast: false };
    await writeFile(`${root}/tron-resource-preflight.json`, JSON.stringify(result, null, 2));
    console.log(JSON.stringify({ mode, sponsorTrxSun: before.sponsor.account.balance, quote, cloneCreationBytes: result.cloneCreationBytes, broadcast: false }));
  } else if (mode === '--setup') {
    if (!state.setup.deploy) assert((await snapshot()).sponsor.account.balance >= 900_000_000, 'Need free test TRX for clone deployment and both resource pools');
    const reference = { contract: await query('getcontract', { value: USDT, visible: true }), contractInfo: await query('getcontractinfo', { value: USDT, visible: true }) };
    const creation = fixtureCreation(reference);
    await setupStep('deploy', options => tron.transactionBuilder.createSmartContract({ ...options, ...creation,
      parameters: [FIXTURE_SUPPLY.toString(), FIXTURE_NAME, FIXTURE_SYMBOL, 6], name: 'NileUSDTResourceFixture',
      feeLimit: DEPLOY_FEE_LIMIT, callValue: 0, userFeePercentage: 100, originEnergyLimit: 1 }, addresses.sponsor), DEPLOY_FEE_LIMIT + 20_000_000);
    state.token = tron.address.fromHex(state.setup.deploy.info.contract_address); await checkFixture(); await save();
    await setupStep('fundUser', async options => (await tron.transactionBuilder.triggerSmartContract(state.token, 'transfer(address,uint256)',
      { ...options, txLocal: true, feeLimit: 15_000_000 }, [{ type: 'address', value: addresses.user }, { type: 'uint256', value: USER_FUND_UNITS.toString() }], addresses.sponsor)).transaction, 16_000_000);
    assert.equal(BigInt(`0x${await constant('balanceOf(address)', wordAddress(addresses.user))}`), USER_FUND_UNITS);
    if (!state.resourcePlan) {
      const simulation = await simulate();
      const unsigned = await buildTransfer(addresses, await query('getnowblock'), state.token);
      state.resourcePlan = resourcePlan(await snapshot(), simulation.energy, bandwidthOf(unsigned));
      assert(state.resourcePlan.stakeEnergy + state.resourcePlan.stakeBandwidth <= 600_000_000, 'Resource setup exceeds test stake cap');
      assert((await snapshot()).sponsor.account.balance >= state.resourcePlan.stakeEnergy + state.resourcePlan.stakeBandwidth + 20_000_000, 'Insufficient remaining setup reserve');
      await save();
    }
    for (const resource of ['ENERGY', 'BANDWIDTH']) {
      const suffix = resource === 'ENERGY' ? 'Energy' : 'Bandwidth';
      const stake = state.resourcePlan[`stake${suffix}`], amount = state.resourcePlan[`additional${suffix}`];
      if (stake) await setupStep(`stake${suffix}`, options => tron.transactionBuilder.freezeBalanceV2(stake, resource, addresses.sponsor, options), 3_000_000, true);
      if (amount) await setupStep(`delegate${suffix}`, async options => {
        const max = resource === 'BANDWIDTH' ? unusedSponsorBandwidthStake((await snapshot()).sponsor)
          : (await query('getcandelegatedmaxsize', { owner_address: hex(addresses.sponsor), type: ResourceType[resource] })).max_size ?? 0;
        assert(max >= amount, 'Delegatable pool changed; inspect before increasing stake');
        return tron.transactionBuilder.delegateResource(amount, addresses.user, resource, addresses.sponsor, false, undefined, options);
      }, 3_000_000, true);
    }
    requirePlatformResources(await snapshot(), state.resourcePlan.energy, state.resourcePlan.bandwidth);
    state.setupComplete = true; await save();
  } else if (mode === '--prepare') {
    assert(state.setupComplete && !state.order, 'Setup required; do not replace an existing order');
    await checkFixture();
    const simulation = await simulate(); const unsigned = await buildTransfer(addresses, await query('getnowblock'), state.token);
    checkTransfer(unsigned, addresses, state.token); const before = await snapshot();
    requirePlatformResources(before, simulation.energy, bandwidthOf(unsigned));
    state.order = { reference: randomUUID(), addresses, unsigned, before, simulation }; await save();
  } else if (mode === '--submit') {
    assert(state.order);
    if (!state.payment) {
      await checkFixture();
      const signed = JSON.parse(await readFile(`${root}/tron-resource-wallet-signed.json`, 'utf8'));
      const signature = verifySignature(state.order, signed, Date.now(), state.token);
      const simulation = await simulate(), before = await snapshot();
      const resources = requirePlatformResources(before, simulation.energy, signature.bandwidthEstimate);
      state.payment = { signed, before, simulation, signature, resources, status: 'signed' }; await save();
    }
    if (state.payment.status !== 'solidified') requirePlatformResources(await snapshot(), state.payment.simulation.energy, state.payment.signature.bandwidthEstimate);
    await submit(state.payment); await audit();
  } else if (mode === '--replay') {
    assert.equal(state.payment?.status, 'solidified');
    const before = await snapshot(), response = await query('broadcasttransaction', state.payment.signed), after = await snapshot();
    assert.equal(response.code, 'DUP_TRANSACTION_ERROR');
    for (const role of ['user', 'sponsor']) {
      assert.equal(after[role].tokenUnits, before[role].tokenUnits);
      assert.equal(after[role].account.balance ?? 0, before[role].account.balance ?? 0);
    }
    state.replay = { observedAt: new Date().toISOString(), response, balancesUnchanged: true }; await save(); await audit();
  } else await audit();
  if (mode !== '--inspect') console.log(JSON.stringify({ mode, token: state.token, setupComplete: state.setupComplete, txID: state.order?.unsigned.txID, checks: state.payment?.checks }));
} finally { await lock.close(); await unlink(path + '.lock'); }
