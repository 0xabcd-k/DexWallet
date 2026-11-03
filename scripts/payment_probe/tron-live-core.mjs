import assert from 'node:assert/strict';
import { TronWeb, utils } from 'tronweb';

export const ENDPOINT = 'https://nile.trongrid.io';
export const CHAIN_ID = '0xcd8690dc';
export const AMOUNT_SUN = 1_000; // 0.001 test TRX, principal only.
export const STAKE_SUN = 700_000_000;
export const TOP_UP_STAKE_SUN = 280_000_000;
export const DELEGATE_SUN = 500_000_000;
// Test pool preparation: force this platform-only transaction to burn TRX rather
// than consume the small stake quota needed for the subsequent delegation.
export const SETUP_MEMO = 'Nile bandwidth pool setup '.padEnd(512, '.');
export const MAX_SETUP_FEE_SUN = 5_000_000;
export const TTL_MS = 600_000;
export const RESULT_BANDWIDTH = 64;
export const BANDWIDTH_MARGIN = 32;
export const ContractType = { Transfer: 'TransferContract', Stake: 'FreezeBalanceV2Contract', Delegate: 'DelegateResourceContract' };
export const tron = new TronWeb({ fullHost: ENDPOINT });
export const hex = address => TronWeb.address.toHex(address).toLowerCase();
export function headerOptions(block) {
  const timestamp = block.block_header.raw_data.timestamp;
  return { blockHeader: { ref_block_bytes: block.blockID.slice(12, 16), ref_block_hash: block.blockID.slice(16, 32), timestamp, expiration: timestamp + TTL_MS } };
}
export function checkTransfer(transaction, addresses) {
  assert.equal(transaction.raw_data.contract.length, 1, 'Exactly one transfer required');
  const contract = transaction.raw_data.contract[0];
  assert.equal(contract.type, ContractType.Transfer);
  assert.equal(contract.Permission_id ?? 0, 0, 'Only the dedicated owner permission is accepted');
  assert.equal(contract.parameter.type_url, 'type.googleapis.com/protocol.TransferContract');
  assert.deepEqual(contract.parameter.value, { to_address: hex(addresses.sponsor), owner_address: hex(addresses.user), amount: AMOUNT_SUN }, 'Transfer differs from saved intent');
  assert.equal(transaction.raw_data.data, undefined, 'No memo in this probe');
  assert.equal(transaction.raw_data.fee_limit, undefined, 'TRX transfer does not have a separate fee payer or Energy limit');
  assert(utils.transaction.txCheck(transaction), 'JSON, protobuf bytes and txID disagree');
}
export function verifyWalletSignature(order, signed, now) {
  checkTransfer(signed, order.addresses);
  assert.equal(signed.txID, order.unsigned.txID, 'Wallet changed transaction hash');
  assert.equal(signed.raw_data_hex, order.unsigned.raw_data_hex, 'Wallet changed signed bytes');
  assert.deepEqual(signed.raw_data, order.unsigned.raw_data, 'Wallet changed transaction fields');
  assert(now >= signed.raw_data.timestamp && now < signed.raw_data.expiration, 'Transaction outside validity interval');
  assert.equal(signed.signature?.length, 1, 'Exactly one user signature required');
  assert.equal(tron.trx.ecRecover(signed), order.addresses.user, 'Wrong wallet signer');
  const pb = utils.transaction.txJsonToPb(signed);
  pb.addSignature(Buffer.from(signed.signature[0], 'hex'));
  return { signatureVerified: true, bandwidthEstimate: pb.serializeBinary().length + RESULT_BANDWIDTH };
}
export function requireDelegatedBandwidth(snapshot, bandwidthEstimate) {
  const user = snapshot.user;
  assert(user.account.address, 'User must be activated');
  assert.equal(user.account.balance, AMOUNT_SUN, 'User must hold principal only');
  assert(user.account.frozenV2?.every(item => !item.amount), 'User must not have its own stake');
  assert.equal(user.account.acquired_delegated_frozenV2_balance_for_bandwidth, DELEGATE_SUN);
  const available = (user.resources.NetLimit ?? 0) - (user.resources.NetUsed ?? 0);
  assert(available >= bandwidthEstimate + BANDWIDTH_MARGIN, 'Insufficient platform-delegated bandwidth; do not broadcast');
  const grants = snapshot.delegation.delegatedResource ?? [];
  assert.equal(grants.length, 1);
  assert.equal(grants[0].frozen_balance_for_bandwidth, DELEGATE_SUN);
  assert.equal(hex(grants[0].from), hex(snapshot.addresses.sponsor));
  assert.equal(hex(grants[0].to), hex(snapshot.addresses.user));
  return { available, bandwidthEstimate, margin: BANDWIDTH_MARGIN };
}
export function verifyPayment(order, signed, info, transaction, before, after) {
  assert.equal(info.id, signed.txID); assert.equal(transaction.txID, signed.txID);
  assert.deepEqual(transaction.ret.map(result => result.contractRet), ['SUCCESS']);
  assert.notEqual(info.result, 'FAILED');
  assert.equal(transaction.raw_data_hex, signed.raw_data_hex);
  // TronLink returns an uppercase recovery byte; the node normalizes hex casing.
  assert.deepEqual(transaction.signature.map(value => Buffer.from(value, 'hex')),
    signed.signature.map(value => Buffer.from(value, 'hex')));
  checkTransfer(transaction, order.addresses);
  assert.equal(info.fee ?? 0, 0, 'User burned TRX');
  assert.equal(info.receipt.net_fee ?? 0, 0);
  assert.equal(info.receipt.energy_usage_total ?? 0, 0);
  const bandwidth = info.receipt.net_usage;
  assert(bandwidth > 0, 'No onchain bandwidth receipt');
  assert.equal(before.user.account.balance, AMOUNT_SUN);
  assert.equal(after.user.account.balance ?? 0, 0);
  assert.equal(after.sponsor.account.balance - before.sponsor.account.balance, AMOUNT_SUN);
  assert.equal(after.user.resources.freeNetUsed ?? 0, before.user.resources.freeNetUsed ?? 0, 'Free user bandwidth was used');
  const used = (after.user.resources.NetUsed ?? 0) - (before.user.resources.NetUsed ?? 0);
  // RPC account reads are live; account usage recovers over 24 hours. The receipt is authoritative.
  assert(used > 0 && used <= bandwidth && bandwidth - used <= 2, 'Delegated bandwidth change does not reconcile');
  return { principalOnly: true, freeBandwidthUnchanged: true, delegatedBandwidthUsed: bandwidth, observedNetUsedIncrease: used, burnedSun: 0 };
}
