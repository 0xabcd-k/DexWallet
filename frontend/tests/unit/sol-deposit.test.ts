import { createHash } from 'node:crypto';
import beneficiary from '../../../backend/logic/chainassets/testdata/sol-beneficiary-deposit.json';
import created from '../../../backend/logic/chainassets/testdata/usdc-self-deposit-create.json';
import existing from '../../../backend/logic/chainassets/testdata/usdc-self-deposit-existing.json';
import usdtCreated from '../../../backend/logic/chainassets/testdata/test-usdt-self-deposit-create.json';
import usdtExisting from '../../../backend/logic/chainassets/testdata/test-usdt-self-deposit-existing.json';
import { afterEach, expect, it, vi } from 'vitest';
import type { DepositQuoteData, SolDepositOrderData } from '../../packages/api-contract/src/platform';
import { authorizeSolDeposit, checkedSolMessage } from '../../apps/user/src/app/native/chain-transfer/sol-deposit';
import { mnemonic } from './local-wallet-fixture';
import * as client from '../../apps/user/src/api/account';

const vault = vi.hoisted(() => ({ addListener: vi.fn(), protect: vi.fn(), unprotect: vi.fn(), acknowledge: vi.fn(), get: vi.fn() }));
vi.mock('../../apps/user/src/app/native/local-wallet/bridge', () => ({ nativeVault: vault }));
afterEach(() => { vi.restoreAllMocks(); vi.clearAllMocks(); });

// Independent fixed SystemProgram transfer + order memo vector with the public BIP39 test wallet.
const now = Date.now();
const quote: DepositQuoteData = { quoteIdx: '1'.repeat(32), solOrderIdx: '2'.repeat(32), walletAddress: 'HAgk14JpMQLgt6rVgv7cBQFJWFto5Dqxi472uT3DKpqk',
  solFeePayer: 'DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN', solRecipient: '7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5',
  assetId: 'solana-devnet:sol', gasAssetId: 'solana-devnet:sol', targetUsd: '0.01', netUsd: '0.01', usdPerCoin: '100', payQuantity: '0.00011',
  estimatedGas: '0.00001', maxGas: '0.00001', gasUsd: '0.001', rateVersion: 'test', asOf: now, quotedAt: now, expiresAt: now + 30000,
  messageSha256: 'dcd09fa7d4f66ba3de5f0e1cf615fb13c3a04e13361a919b7d5869f23ce1c922', canSubmit: false };
const order: SolDepositOrderData = { assetId: quote.assetId, quoteIdx: quote.quoteIdx!, orderIdx: quote.solOrderIdx!, state: 'awaiting_signature', executionState: 'waiting',
  messageBase64: 'AgACBbtu5UznH0IODqQvtEDIOd4fnBCsFVW9DjOSQOLewUk18DYnYkanW53jNJ7UKxXiMvZRj8IPX81PHWToH5vSWPdet76rPhnRbUWumOzXkErufS+rzXYQx2sGbk6vvio4ngAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABUpTWpkpIQZNJOhxYNo4fHw1td28kruB5B+oQEEFRI3QJFNbwCH4iosm+L1MVUyRP1PvIerM6xlFR9kfaRhbXQIDAgECDAIAAACwrQEAAAAAAAQAMmRleHdhbGxldDpkZXBvc2l0OjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIy', messageSha256: quote.messageSha256!, lastValidBlockHeight: 120, quoteExpiresAt: quote.expiresAt, reviewAfter: quote.expiresAt + 60000 };
const signature = 'mfAWpqfGcCpZYmLdaC5hERoYNF4VpSuv3xqdViFw5FPcB6FouimYwbovc3ES4GDCwuD2RHVkV4aw7ebgYqceXRd';

it('opens the order before device unlock and submits only the user signature', async () => {
  const sequence: string[] = [];
  vault.addListener.mockResolvedValue({ remove: vi.fn() });
  vault.protect.mockResolvedValue({ active: true, revision: 1 });
  vault.get.mockImplementation(async () => {
    sequence.push('unlock');
    return { secret: JSON.stringify({ version: 1, scheme: 'bip39', tonVersion: 'v5r1', words: mnemonic }) };
  });
  vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode, options) => {
    sequence.push(path.split('/').pop()!);
    if (path.endsWith('/prepare')) return decode({ data: order });
    expect(options?.body).toEqual({ quoteIdx: quote.quoteIdx, userSignature: signature });
    return decode({ data: { ...order, state: 'accepted', acceptedAt: now + 1 } });
  });
  const accepted = await authorizeSolDeposit('userIdx','walletId',quote, Date.now() + 30000, new AbortController().signal);
  expect(accepted.state).toBe('accepted');
  expect(sequence).toEqual(['prepare','unlock','authorize']);
  expect(vault.unprotect).toHaveBeenCalledOnce();
});

it('rejects changes to the transfer amount, accounts, and order memo before device unlock', () => {
  expect(checkedSolMessage(quote, order).byteLength).toBeGreaterThan(200);
  for (const q of [{ ...quote, payQuantity: '0.00012' }, { ...quote, solRecipient: quote.walletAddress! },
    { ...quote, solFeePayer: quote.solRecipient! }, { ...quote, maxGas: '0.1' }]) {
    expect(() => checkedSolMessage(q, order)).toThrow();
  }
  const changed = '3'.repeat(32);
  expect(() => checkedSolMessage({ ...quote, solOrderIdx: changed }, { ...order, orderIdx: changed })).toThrow();
  expect(vault.get).not.toHaveBeenCalled();
});

it.each([created.quote, existing.quote, usdtCreated.quote, usdtExisting.quote])('verifies the finalized $AssetID signing message with storage funding $AccountFunding', f => {
  const q = { ...quote, assetId: f.AssetID, quoteIdx: f.QuoteIdx, solOrderIdx: f.SolOrderIdx, walletAddress: f.WalletAddress,
    solFeePayer: f.FeePayer, solRecipient: f.Recipient, payQuantity: f.PayQuantity, accountFunding: f.AccountFunding, messageSha256: f.MessageSHA256 };
  const o = { ...order, assetId: q.assetId, quoteIdx: f.QuoteIdx, orderIdx: f.SolOrderIdx, messageBase64: f.SolMessage, messageSha256: f.MessageSHA256 };
  expect(checkedSolMessage(q, o).length).toBeGreaterThan(300);
  for (const changes of [{ payQuantity: '0.012' }, { solRecipient: quote.walletAddress! }, { accountFunding: f.AccountFunding === '0' ? '0.001' : '0' }]) {
    expect(() => checkedSolMessage({ ...q, ...changes }, o)).toThrow();
  }
  for (const assetId of ['solana-devnet:usdt:' + f.WalletAddress, 'solana-devnet:usdt:4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU']) {
    expect(() => checkedSolMessage({ ...q, assetId }, { ...o, assetId })).toThrow();
  }
  const alteredMint = Buffer.from(f.SolMessage, 'base64');
  const mintOffset = 4 + 32 * (f.AccountFunding === '0' ? 5 : 6);
  alteredMint[mintOffset] = alteredMint[mintOffset]! ^ 1;
  const hash = createHash('sha256').update(alteredMint).digest('hex');
  expect(() => checkedSolMessage({ ...q, messageSha256: hash }, { ...o, messageSha256: hash, messageBase64: alteredMint.toString('base64') })).toThrow();
  const changed = 'a'.repeat(32);
  expect(() => checkedSolMessage({ ...q, solOrderIdx: changed }, { ...o, orderIdx: changed })).toThrow();
});

it('verifies the Devnet beneficiary payment and refuses reassignment or conversion to a self-deposit', () => {
  const q = { ...quote, beneficiaryIdx: beneficiary.beneficiaryIdx, quoteIdx: beneficiary.quoteIdx, solOrderIdx: beneficiary.orderIdx,
    walletAddress: beneficiary.walletAddress, solFeePayer: beneficiary.feePayer, solRecipient: beneficiary.recipient,
    payQuantity: beneficiary.payQuantity, messageSha256: beneficiary.messageSha256 };
  const o = { ...order, beneficiaryIdx: q.beneficiaryIdx, quoteIdx: q.quoteIdx, orderIdx: q.solOrderIdx,
    messageBase64: beneficiary.messageBase64, messageSha256: q.messageSha256 };
  expect(Buffer.from(checkedSolMessage(q, o)).toString('base64')).toBe(beneficiary.messageBase64);
  const { beneficiaryIdx: _q, ...selfQuote } = q, { beneficiaryIdx: _o, ...selfOrder } = o;
  expect(() => checkedSolMessage(q, selfOrder)).toThrow();
  expect(() => checkedSolMessage(selfQuote, selfOrder)).toThrow();
  const other = '09876543210987654';
  expect(() => checkedSolMessage({ ...q, beneficiaryIdx: other }, { ...o, beneficiaryIdx: other })).toThrow();
});
