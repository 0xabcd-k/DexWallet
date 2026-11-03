// Read-only Nile USDT preflight. No key access, wallet prompts or broadcasts.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { post, rpc } from './rpc.mjs';
import { ENDPOINT, CHAIN_ID, hex, tron } from './tron-live-core.mjs';
import { USDT, MAINNET_USDT, AMOUNT_UNITS, wordAddress, transferData, buildTransfer, checkTransfer, simulationEnergy } from './tron-usdt-core.mjs';

const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const addresses = JSON.parse(await readFile(`${root}/tron-live-addresses.json`, 'utf8'));
assert.deepEqual(addresses, { user: 'TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio', sponsor: 'TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH' });
async function query(method, body = {}, endpoint = ENDPOINT) {
  const value = await post(`${endpoint}/wallet/${method}`, body, { direct: true });
  assert(!value.Error, `RPC ${method}: ${value.Error}`); return value;
}
function contractSummary(c) {
  assert(c.contract_address && c.origin_address && c.bytecode && c.abi);
  return { address: c.contract_address, origin: c.origin_address, consumeUserResourcePercent: c.consume_user_resource_percent ?? 0,
    originEnergyLimit: c.origin_energy_limit, codeHash: c.code_hash,
    bytecodeSha256: createHash('sha256').update(Buffer.from(c.bytecode, 'hex')).digest('hex'),
    transferABI: c.abi.entrys.find(entry => entry.type === 'Function' && entry.name === 'transfer') };
}
function resources(r) {
  return Object.fromEntries(['NetLimit', 'NetUsed', 'freeNetLimit', 'freeNetUsed', 'EnergyLimit', 'EnergyUsed',
    'TotalNetLimit', 'TotalNetWeight', 'TotalEnergyLimit', 'TotalEnergyWeight'].map(key => [key, r[key] ?? 0]));
}
async function constant(selector, parameter = '') {
  const response = await query('triggerconstantcontract', { owner_address: hex(addresses.user),
    contract_address: hex(USDT), function_selector: selector, parameter });
  assert.equal(response.result?.result, true); assert.equal(response.constant_result?.length, 1);
  assert(!response.transaction?.ret?.some(item => item.ret === 'FAILED' || (item.contractRet && item.contractRet !== 'SUCCESS')));
  return response.constant_result[0];
}
const mainnet = 'https://api.trongrid.io'; // Comparison is read-only; no transaction is built for Mainnet.
assert.equal(await rpc(ENDPOINT + '/jsonrpc', 'eth_chainId', [], { direct: true }), CHAIN_ID);
assert.equal(await rpc(mainnet + '/jsonrpc', 'eth_chainId', [], { direct: true }), '0x2b6653dc');
const [nileContract, mainnetContract, block, info, userAccount, sponsorAccount, userResources, sponsorResources, userUnits, sponsorUnits, decimals, symbol] = await Promise.all([
  query('getcontract', { value: USDT, visible: true }), query('getcontract', { value: MAINNET_USDT, visible: true }, mainnet),
  query('getnowblock'), query('getcontractinfo', { value: USDT, visible: true }),
  query('getaccount', { address: addresses.user, visible: true }), query('getaccount', { address: addresses.sponsor, visible: true }),
  query('getaccountresource', { address: addresses.user, visible: true }), query('getaccountresource', { address: addresses.sponsor, visible: true }),
  constant('balanceOf(address)', wordAddress(addresses.user)), constant('balanceOf(address)', wordAddress(addresses.sponsor)),
  constant('decimals()'), constant('symbol()'),
]);
assert.equal(BigInt(`0x${decimals}`), 6n);
assert.equal(tron.utils.abi.decodeParams([], ['string'], `0x${symbol}`)[0], 'USDT');
const [nileOrigin, mainnetOrigin] = await Promise.all([
  query('getaccountresource', { address: nileContract.origin_address, visible: true }),
  query('getaccountresource', { address: mainnetContract.origin_address, visible: true }, mainnet),
]);
const unsigned = await buildTransfer(addresses, block); checkTransfer(unsigned, addresses);
const simulation = await query('triggerconstantcontract', { owner_address: hex(addresses.user), contract_address: hex(USDT),
  data: transferData(addresses.sponsor) });
let validEnergyEstimate, simulationError;
try { validEnergyEstimate = simulationEnergy(simulation, addresses); } catch (error) { simulationError = error.message; }
const userBalance = BigInt(`0x${userUnits}`);
const output = { observedAt: new Date().toISOString(), network: 'Nile', chainId: CHAIN_ID, addresses, token: USDT,
  amountUnits: AMOUNT_UNITS.toString(), decimals: 6, symbol: 'USDT', block: block.block_header.raw_data.number,
  nileContract: contractSummary(nileContract), mainnetContract: contractSummary(mainnetContract), contractState: info.contract_state,
  user: { trxSun: userAccount.balance ?? 0, tokenUnits: userBalance.toString(), resources: resources(userResources) },
  sponsor: { trxSun: sponsorAccount.balance ?? 0, tokenUnits: BigInt(`0x${sponsorUnits}`).toString(), resources: resources(sponsorResources) },
  origins: { nile: resources(nileOrigin), mainnet: resources(mainnetOrigin) },
  simulation, validEnergyEstimate, simulationError,
  status: userBalance < AMOUNT_UNITS ? 'awaiting_test_usdt' : validEnergyEstimate ? 'simulation_passed_not_broadcast' : 'simulation_failed',
  transactionBuiltLocally: true, broadcast: false, platformEnergySponsorshipProven: false,
};
await writeFile(`${root}/tron-usdt-preflight.json`, JSON.stringify(output, null, 2) + '\n');
console.log(JSON.stringify({ status: output.status, userTokenUnits: output.user.tokenUnits, userTrxSun: output.user.trxSun,
  validEnergyEstimate, simulationError, nileUserEnergyPercent: output.nileContract.consumeUserResourcePercent,
  mainnetUserEnergyPercent: output.mainnetContract.consumeUserResourcePercent, broadcast: false }));
if (output.status !== 'simulation_passed_not_broadcast') process.exitCode = 2;
