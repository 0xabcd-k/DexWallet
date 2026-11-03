import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { PublicKey, Transaction } from '@solana/web3.js';
import {
  ACCOUNT_SIZE, TOKEN_PROGRAM_ID, getAssociatedTokenAddressSync,
  createAssociatedTokenAccountIdempotentInstruction, createTransferCheckedInstruction,
} from '@solana/spl-token';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT, MAX_FEE_LAMPORTS, digest } from './solana-live-core.mjs';

export const MAX_ATA_RENT_LAMPORTS = 3000000;

export function tokenAddresses(addresses, asset) {
  return Object.fromEntries(['user', 'recipient'].map(role => [role,
    getAssociatedTokenAddressSync(new PublicKey(asset.mint), new PublicKey(addresses[role])).toBase58(),
  ]));
}

function tokenAccountInfo(account, owner, asset) {
  assert.equal(account.owner, TOKEN_PROGRAM_ID.toBase58(), 'Wrong token program');
  assert.equal(account.data.parsed.type, 'account', 'Not a token account');
  const info = account.data.parsed.info;
  assert.equal(info.mint, asset.mint, 'Wrong token mint');
  assert.equal(info.owner, owner, 'Wrong token authority');
  assert.equal(info.state, 'initialized', 'Token account is frozen or uninitialized');
  assert.equal(info.tokenAmount.decimals, asset.decimals, 'Wrong token decimals');
  return info;
}

export function validateTokenSnapshot(addresses, accounts, rentLamports, asset) {
  const [mint, source, recipient] = accounts.value;
  assert(mint, 'Token mint missing');
  assert.equal(mint.owner, TOKEN_PROGRAM_ID.toBase58(), 'Wrong mint program');
  assert.equal(mint.data.parsed.type, 'mint', 'Not a mint account');
  assert.equal(mint.data.parsed.info.isInitialized, true, 'Mint not initialized');
  assert.equal(mint.data.parsed.info.decimals, asset.decimals, 'Wrong mint decimals');
  assert(source, asset.fundingHint);
  const sourceInfo = tokenAccountInfo(source, addresses.user, asset);
  assert(BigInt(sourceInfo.tokenAmount.amount) >= asset.amount, 'Insufficient test tokens');
  const recipientInfo = recipient ? tokenAccountInfo(recipient, addresses.recipient, asset) : null;
  assert(Number.isSafeInteger(rentLamports) && rentLamports > 0 && rentLamports <= MAX_ATA_RENT_LAMPORTS, 'ATA rent exceeds probe limit');
  return {
    observedAt: new Date().toISOString(), slot: accounts.context.slot,
    sourceAmount: sourceInfo.tokenAmount.amount, recipientAmount: recipientInfo?.tokenAmount.amount ?? '0',
    recipientExists: recipient !== null, rentLamports,
  };
}

export async function inspectTokenAccounts(addresses, asset) {
  const atas = tokenAddresses(addresses, asset);
  const [accounts, rent] = await Promise.all([
    rpc(DEVNET_ENDPOINT, 'getMultipleAccounts', [[asset.mint, atas.user, atas.recipient], { commitment: 'confirmed', encoding: 'jsonParsed' }]),
    rpc(DEVNET_ENDPOINT, 'getMinimumBalanceForRentExemption', [ACCOUNT_SIZE, { commitment: 'confirmed' }]),
  ]);
  return validateTokenSnapshot(addresses, accounts, rent, asset);
}

export function prepareTokenOrder(addresses, block, snapshot, existingAta, asset) {
  assert.equal(snapshot.recipientExists, existingAta, 'Recipient ATA does not match the requested test case');
  const atas = tokenAddresses(addresses, asset);
  const mint = new PublicKey(asset.mint), payer = new PublicKey(addresses.sponsor);
  const transaction = new Transaction({ feePayer: payer, recentBlockhash: block.blockhash }).add(
    createAssociatedTokenAccountIdempotentInstruction(payer, new PublicKey(atas.recipient), new PublicKey(addresses.recipient), mint),
    createTransferCheckedInstruction(new PublicKey(atas.user), mint, new PublicKey(atas.recipient), new PublicKey(addresses.user), asset.amount, asset.decimals),
  );
  return {
    reference: randomUUID(), preparedAt: new Date().toISOString(), network: 'solana-devnet', asset: asset.symbol,
    probeCase: `${asset.probePrefix}-${existingAta ? 'existing' : 'new'}-ata`,
    addresses, tokenAccounts: atas, mint: asset.mint, tokenProgram: TOKEN_PROGRAM_ID.toBase58(),
    amountAtomic: asset.amount.toString(), decimals: asset.decimals, accountSnapshot: snapshot,
    estimatedAtaRentLamports: existingAta ? 0 : snapshot.rentLamports, ...block,
    unsigned: transaction.serialize({ requireAllSignatures: false }).toString('base64'),
    messageSha256: digest(transaction.serializeMessage()), status: 'prepared',
  };
}

export function verifyTokenReceipt(order, receipt, asset) {
  assert.equal(order.asset, asset.symbol, 'Unexpected probe asset');
  assert.equal(order.mint, asset.mint, 'Unexpected order mint');
  assert.equal(order.amountAtomic, asset.amount.toString(), 'Unexpected order amount');
  assert(receipt?.meta, 'Confirmed transaction details unavailable');
  const { meta, transaction } = receipt;
  assert.equal(meta.err, null, 'Transaction execution failed');
  const keys = transaction.message.accountKeys;
  const index = address => { const i = keys.indexOf(address); assert(i >= 0, 'Expected account missing'); return i; };
  const delta = address => { const i = index(address); return meta.postBalances[i] - meta.preBalances[i]; };
  const tokenAmount = (balances, address, owner, allowMissing = false) => {
    const entry = balances.find(item => item.accountIndex === index(address));
    if (!entry) { assert(allowMissing, 'Missing token balance in receipt'); return 0n; }
    assert.equal(entry.mint, asset.mint, 'Receipt token mint mismatch');
    assert.equal(entry.owner, owner, 'Receipt token authority mismatch');
    assert.equal(entry.programId, TOKEN_PROGRAM_ID.toBase58(), 'Receipt token program mismatch');
    assert.equal(entry.uiTokenAmount.decimals, asset.decimals, 'Receipt token decimals mismatch');
    return BigInt(entry.uiTokenAmount.amount);
  };
  const sourceBefore = tokenAmount(meta.preTokenBalances, order.tokenAccounts.user, order.addresses.user);
  const sourceAfter = tokenAmount(meta.postTokenBalances, order.tokenAccounts.user, order.addresses.user);
  const recipientIndex = index(order.tokenAccounts.recipient);
  const created = meta.preBalances[recipientIndex] === 0;
  const recipientBefore = tokenAmount(meta.preTokenBalances, order.tokenAccounts.recipient, order.addresses.recipient, created);
  const recipientAfter = tokenAmount(meta.postTokenBalances, order.tokenAccounts.recipient, order.addresses.recipient);
  assert.equal(sourceAfter - sourceBefore, -BigInt(order.amountAtomic), 'Incorrect token sender debit');
  assert.equal(recipientAfter - recipientBefore, BigInt(order.amountAtomic), 'Incorrect token recipient credit');
  assert.equal(keys[0], order.addresses.sponsor, 'Unexpected fee payer');
  assert.equal(delta(order.addresses.user), 0, 'User paid native SOL');
  assert.equal(delta(order.addresses.recipient), 0, 'Recipient wallet SOL changed');
  assert.equal(delta(order.tokenAccounts.user), 0, 'Source ATA rent changed');
  const rent = delta(order.tokenAccounts.recipient);
  assert.equal(rent, created ? order.accountSnapshot.rentLamports : 0, 'ATA storage deposit mismatch');
  assert(rent <= MAX_ATA_RENT_LAMPORTS && meta.fee <= MAX_FEE_LAMPORTS, 'Sponsor cost exceeds probe limit');
  assert.equal(delta(order.addresses.sponsor), -(meta.fee + rent), 'Sponsor did not pay exactly fee plus ATA rent');
  return {
    senderTokenDeltaAtomic: (sourceAfter - sourceBefore).toString(), recipientTokenDeltaAtomic: (recipientAfter - recipientBefore).toString(),
    userSolDeltaLamports: delta(order.addresses.user), sponsorDeltaLamports: delta(order.addresses.sponsor),
    feeLamports: meta.fee, ataRentLamports: rent, recipientAtaCreated: created,
    feePayer: keys[0], mint: asset.mint,
  };
}
