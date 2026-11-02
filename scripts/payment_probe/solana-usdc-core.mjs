import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { PublicKey, Transaction } from '@solana/web3.js';
import {
  ACCOUNT_SIZE, TOKEN_PROGRAM_ID, getAssociatedTokenAddressSync,
  createAssociatedTokenAccountIdempotentInstruction, createTransferCheckedInstruction,
} from '@solana/spl-token';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT, MAX_FEE_LAMPORTS, digest } from './solana-live-core.mjs';

export const USDC_MINT = '4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU';
export const USDC_DECIMALS = 6;
export const USDC_AMOUNT = 10000n; // 0.01 test USDC, not a product minimum.
export const MAX_ATA_RENT_LAMPORTS = 3000000;

export function tokenAddresses(addresses) {
  return Object.fromEntries(['user', 'recipient'].map(role => [role,
    getAssociatedTokenAddressSync(new PublicKey(USDC_MINT), new PublicKey(addresses[role])).toBase58(),
  ]));
}

function tokenAccountInfo(account, owner) {
  assert.equal(account.owner, TOKEN_PROGRAM_ID.toBase58(), 'Wrong token program');
  assert.equal(account.data.parsed.type, 'account', 'Not a token account');
  const info = account.data.parsed.info;
  assert.equal(info.mint, USDC_MINT, 'Wrong token mint');
  assert.equal(info.owner, owner, 'Wrong token authority');
  assert.equal(info.state, 'initialized', 'Token account is frozen or uninitialized');
  assert.equal(info.tokenAmount.decimals, USDC_DECIMALS, 'Wrong token decimals');
  return info;
}

export function validateUsdcSnapshot(addresses, accounts, rentLamports) {
  const [mint, source, recipient] = accounts.value;
  assert(mint, 'USDC mint missing');
  assert.equal(mint.owner, TOKEN_PROGRAM_ID.toBase58(), 'Wrong mint program');
  assert.equal(mint.data.parsed.type, 'mint', 'Not a mint account');
  assert.equal(mint.data.parsed.info.isInitialized, true, 'Mint not initialized');
  assert.equal(mint.data.parsed.info.decimals, USDC_DECIMALS, 'Wrong mint decimals');
  assert(source, 'Test user needs Circle Devnet USDC from the official faucet');
  const sourceInfo = tokenAccountInfo(source, addresses.user);
  assert(BigInt(sourceInfo.tokenAmount.amount) >= USDC_AMOUNT, 'Insufficient test USDC');
  const recipientInfo = recipient ? tokenAccountInfo(recipient, addresses.recipient) : null;
  assert(Number.isSafeInteger(rentLamports) && rentLamports > 0 && rentLamports <= MAX_ATA_RENT_LAMPORTS, 'ATA rent exceeds probe limit');
  return {
    observedAt: new Date().toISOString(), slot: accounts.context.slot,
    sourceAmount: sourceInfo.tokenAmount.amount, recipientAmount: recipientInfo?.tokenAmount.amount ?? '0',
    recipientExists: recipient !== null, rentLamports,
  };
}

export async function inspectUsdcAccounts(addresses) {
  const atas = tokenAddresses(addresses);
  const [accounts, rent] = await Promise.all([
    rpc(DEVNET_ENDPOINT, 'getMultipleAccounts', [[USDC_MINT, atas.user, atas.recipient], { commitment: 'confirmed', encoding: 'jsonParsed' }]),
    rpc(DEVNET_ENDPOINT, 'getMinimumBalanceForRentExemption', [ACCOUNT_SIZE, { commitment: 'confirmed' }]),
  ]);
  return validateUsdcSnapshot(addresses, accounts, rent);
}

export function prepareUsdcOrder(addresses, block, snapshot, existingAta) {
  assert.equal(snapshot.recipientExists, existingAta, 'Recipient ATA does not match the requested test case');
  const atas = tokenAddresses(addresses);
  const mint = new PublicKey(USDC_MINT), payer = new PublicKey(addresses.sponsor);
  const transaction = new Transaction({ feePayer: payer, recentBlockhash: block.blockhash }).add(
    createAssociatedTokenAccountIdempotentInstruction(payer, new PublicKey(atas.recipient), new PublicKey(addresses.recipient), mint),
    createTransferCheckedInstruction(new PublicKey(atas.user), mint, new PublicKey(atas.recipient), new PublicKey(addresses.user), USDC_AMOUNT, USDC_DECIMALS),
  );
  return {
    reference: randomUUID(), preparedAt: new Date().toISOString(), network: 'solana-devnet', asset: 'USDC',
    probeCase: existingAta ? 'solana-usdc-existing-ata' : 'solana-usdc-new-ata',
    addresses, tokenAccounts: atas, mint: USDC_MINT, tokenProgram: TOKEN_PROGRAM_ID.toBase58(),
    amountAtomic: USDC_AMOUNT.toString(), decimals: USDC_DECIMALS, accountSnapshot: snapshot,
    estimatedAtaRentLamports: existingAta ? 0 : snapshot.rentLamports, ...block,
    unsigned: transaction.serialize({ requireAllSignatures: false }).toString('base64'),
    messageSha256: digest(transaction.serializeMessage()), status: 'prepared',
  };
}

export function verifyUsdcReceipt(order, receipt) {
  assert(receipt?.meta, 'Confirmed transaction details unavailable');
  const { meta, transaction } = receipt;
  assert.equal(meta.err, null, 'Transaction execution failed');
  const keys = transaction.message.accountKeys;
  const index = address => { const i = keys.indexOf(address); assert(i >= 0, 'Expected account missing'); return i; };
  const delta = address => { const i = index(address); return meta.postBalances[i] - meta.preBalances[i]; };
  const tokenAmount = (balances, address, owner, allowMissing = false) => {
    const entry = balances.find(item => item.accountIndex === index(address));
    if (!entry) { assert(allowMissing, 'Missing token balance in receipt'); return 0n; }
    assert.equal(entry.mint, USDC_MINT, 'Receipt token mint mismatch');
    assert.equal(entry.owner, owner, 'Receipt token authority mismatch');
    assert.equal(entry.programId, TOKEN_PROGRAM_ID.toBase58(), 'Receipt token program mismatch');
    assert.equal(entry.uiTokenAmount.decimals, USDC_DECIMALS, 'Receipt token decimals mismatch');
    return BigInt(entry.uiTokenAmount.amount);
  };
  const sourceBefore = tokenAmount(meta.preTokenBalances, order.tokenAccounts.user, order.addresses.user);
  const sourceAfter = tokenAmount(meta.postTokenBalances, order.tokenAccounts.user, order.addresses.user);
  const recipientIndex = index(order.tokenAccounts.recipient);
  const created = meta.preBalances[recipientIndex] === 0;
  const recipientBefore = tokenAmount(meta.preTokenBalances, order.tokenAccounts.recipient, order.addresses.recipient, created);
  const recipientAfter = tokenAmount(meta.postTokenBalances, order.tokenAccounts.recipient, order.addresses.recipient);
  assert.equal(sourceAfter - sourceBefore, -BigInt(order.amountAtomic), 'Incorrect USDC sender debit');
  assert.equal(recipientAfter - recipientBefore, BigInt(order.amountAtomic), 'Incorrect USDC recipient credit');
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
    feePayer: keys[0], mint: USDC_MINT,
  };
}
