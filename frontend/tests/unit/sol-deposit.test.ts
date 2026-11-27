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
const order: SolDepositOrderData = { quoteIdx: quote.quoteIdx!, orderIdx: quote.solOrderIdx!, state: 'awaiting_signature', executionState: 'waiting',
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
