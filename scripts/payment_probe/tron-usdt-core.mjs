import assert from 'node:assert/strict';
import { utils } from 'tronweb';
import { tron, hex, headerOptions, RESULT_BANDWIDTH } from './tron-live-core.mjs';

export const USDT = 'TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf';
export const MAINNET_USDT = 'TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t';
export const AMOUNT_UNITS = 10_000n; // 0.01 test USDT, six decimals.
export const FEE_LIMIT_SUN = 15_000_000; // Maximum permitted Energy budget, not an estimated fee.
export const SIGNING_TTL_MS = 1_800_000; // Bounded window for interactive wallet inspection.
export const TRANSFER_SELECTOR = 'a9059cbb';
export const TRANSFER_TOPIC = 'ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef';
export const wordAddress = address => hex(address).slice(2).padStart(64, '0');
export const transferData = recipient => TRANSFER_SELECTOR + wordAddress(recipient) + AMOUNT_UNITS.toString(16).padStart(64, '0');

export async function buildTransfer(addresses, block) {
  const options = headerOptions(block);
  options.blockHeader.expiration = options.blockHeader.timestamp + SIGNING_TTL_MS;
  const { transaction } = await tron.transactionBuilder.triggerSmartContract(USDT, 'transfer(address,uint256)', {
    ...options, txLocal: true, feeLimit: FEE_LIMIT_SUN,
  }, [{ type: 'address', value: addresses.sponsor }, { type: 'uint256', value: AMOUNT_UNITS.toString() }], addresses.user);
  return transaction;
}
export function checkTransfer(transaction, addresses) {
  assert.equal(transaction.raw_data.contract.length, 1);
  const contract = transaction.raw_data.contract[0];
  assert.equal(contract.type, 'TriggerSmartContract');
  assert.equal(contract.Permission_id ?? 0, 0);
  assert.equal(contract.parameter.type_url, 'type.googleapis.com/protocol.TriggerSmartContract');
  assert.deepEqual(contract.parameter.value, { data: transferData(addresses.sponsor),
    owner_address: hex(addresses.user), contract_address: hex(USDT) }, 'USDT transfer differs from saved intent');
  assert.equal(transaction.raw_data.fee_limit, FEE_LIMIT_SUN);
  assert.equal(transaction.raw_data.data, undefined, 'No memo');
  assert(utils.transaction.txCheck(transaction), 'JSON, protobuf and txID disagree');
}
export function verifySignature(order, signed, now) {
  checkTransfer(signed, order.addresses);
  assert.equal(signed.txID, order.unsigned.txID);
  assert.equal(signed.raw_data_hex, order.unsigned.raw_data_hex);
  assert.deepEqual(signed.raw_data, order.unsigned.raw_data);
  assert(now >= signed.raw_data.timestamp && now < signed.raw_data.expiration, 'Outside validity interval');
  assert.equal(signed.signature?.length, 1);
  assert.equal(tron.trx.ecRecover(signed), order.addresses.user, 'Wrong user signer');
  const pb = utils.transaction.txJsonToPb(signed);
  pb.addSignature(Buffer.from(signed.signature[0], 'hex'));
  return { signatureVerified: true, bandwidthEstimate: pb.serializeBinary().length + RESULT_BANDWIDTH };
}
export function simulationEnergy(response, addresses) {
  assert.equal(response.result?.result, true, 'RPC did not accept simulation');
  assert(!response.transaction?.ret?.some(item => item.ret === 'FAILED' || (item.contractRet && item.contractRet !== 'SUCCESS')), 'Simulation failed or reverted');
  assert.equal(response.constant_result?.length, 1);
  assert.match(response.constant_result[0], /^[0-9a-fA-F]{64}$/);
  // This fixed Nile USDT implementation returns false even on successful transfer.
  // Accept only a boolean word, the exact simulated call and its exact Transfer event.
  assert([0n, 1n].includes(BigInt(`0x${response.constant_result[0]}`)));
  const contracts = response.transaction?.raw_data?.contract;
  assert.equal(contracts?.length, 1);
  assert.equal(contracts[0].type, 'TriggerSmartContract');
  assert.deepEqual(contracts[0].parameter.value, { owner_address: hex(addresses.user), contract_address: hex(USDT), data: transferData(addresses.sponsor) });
  checkTransferEvent({ log: response.logs }, addresses);
  assert(Number.isSafeInteger(response.energy_used) && response.energy_used > 0);
  return response.energy_used;
}
export function energyAttribution(receipt) {
  const total = receipt.energy_usage_total ?? 0;
  const caller = receipt.energy_usage ?? 0;
  const origin = receipt.origin_energy_usage ?? 0;
  const burnedSun = receipt.energy_fee ?? 0;
  assert(total > 0 && caller >= 0 && origin >= 0 && burnedSun >= 0);
  assert(caller + origin <= total);
  if (burnedSun === 0) assert.equal(caller + origin, total, 'Unaccounted Energy consumption');
  // Caller Energy is not proof of platform sponsorship without delegation and account evidence.
  return { total, caller, origin, burnedSun, originPaidAll: origin === total && caller === 0 && burnedSun === 0 };
}
export function checkTransferEvent(info, addresses) {
  const events = (info.log ?? []).filter(log => log.topics?.[0]?.toLowerCase() === TRANSFER_TOPIC);
  assert.equal(events.length, 1, 'Exactly one Transfer event required');
  const event = events[0];
  assert.equal(event.address.toLowerCase(), hex(USDT).slice(2));
  assert.deepEqual(event.topics.map(value => value.toLowerCase()), [TRANSFER_TOPIC, wordAddress(addresses.user), wordAddress(addresses.sponsor)]);
  assert.match(event.data, /^[0-9a-fA-F]{64}$/);
  assert.equal(BigInt(`0x${event.data}`), AMOUNT_UNITS);
}
