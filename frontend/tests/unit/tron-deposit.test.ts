import { afterEach, expect, it, vi } from 'vitest';
import type { DepositQuoteData } from '../../packages/api-contract/src/platform';
import f from '../../../backend/logic/chainreceipt/testdata/tron-trx-self-deposit.json';
import { authorizeTronDeposit, getTronOrder, nileDepositDigests } from '../../apps/user/src/app/native/chain-transfer/tron-deposit';
import * as client from '../../apps/user/src/api/account';
import { mnemonic } from './local-wallet-fixture';

const vault = vi.hoisted(() => ({ addListener: vi.fn(), protect: vi.fn(), acknowledge: vi.fn(), unprotect: vi.fn(), get: vi.fn() }));
vi.mock('../../apps/user/src/app/native/local-wallet/bridge', () => ({ nativeVault: vault }));
afterEach(() => { vi.restoreAllMocks(); vi.clearAllMocks(); });
const q = f.quote;
const quote: DepositQuoteData = { quoteIdx: q.QuoteIdx, assetId: q.AssetID, gasAssetId: q.GasAssetID, targetUsd: q.TargetUSD,
  estimatedGas: q.EstimatedGas, maxGas: q.MaxGas, gasUsd: q.GasUSD, usdPerCoin: q.USDPerCoin, netUsd: q.NetUSD, payQuantity: q.PayQuantity,
  walletFeePreview: true, walletAddress: q.WalletAddress, tronRecipient: q.Recipient, tronResourceOwner: q.FeePayer,
  tronRefBlock: q.TRONReference.ID, tronRefTime: q.TRONReference.Timestamp, delegatedBandwidth: q.DelegatedBandwidth, messageSha256: q.MessageSHA256,
  asOf: q.AsOf, quotedAt: q.QuotedAt, expiresAt: q.ExpiresAt, rateVersion: q.Version, canSubmit: false };

it('rebuilds the solidified transfer and binds its off-chain authorization to exactly one quote', () => {
  const digest = nileDepositDigests(quote);
  expect(Buffer.from(digest.tx).toString('hex')).toBe(f.evidence.transaction.txID);
  const other = nileDepositDigests({ ...quote, quoteIdx: 'a'.repeat(32) });
  expect(other.tx).toEqual(digest.tx);
  expect(other.order).not.toEqual(digest.order);
  for (const change of [{ maxGas: '1' }, { payQuantity: '0.01' }, { assetId: 'tron-mainnet:trx' }, { tronRefTime: quote.tronRefTime! + 1000 },
    { delegatedBandwidth: q.DelegatedBandwidth - 1 }, { tronRecipient: q.WalletAddress }, { tronResourceOwner: 'T' + '1'.repeat(33) }]) {
    expect(() => nileDepositDigests({ ...quote, ...change })).toThrow();
  }
});
it('signs both TronWeb vectors with one device unlock and submits only these signatures', async () => {
  // Independent TronWeb protobuf and secp256k1 output for the public BIP39 vector, recovery byte 0/1.
  const testQuote = { ...quote, walletAddress: 'TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH',
    messageSha256: 'ef74726e0853f4bf1ad13d7904808ce539991a3ef57bde527005ddff977924a8' };
  vault.protect.mockResolvedValue({ active: true, revision: 1 });
  vault.addListener.mockResolvedValue({ remove: vi.fn() });
  vault.get.mockResolvedValue({ secret: JSON.stringify({ version: 1, scheme: 'bip39', tonVersion: 'v5r1', words: mnemonic }) });
  const request = vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode, options) => {
    if (path.endsWith('/' + q.QuoteIdx)) return decode({ data: { quoteIdx: q.QuoteIdx, state: 'awaiting_signature' } });
    expect(options?.body).toEqual({ quoteIdx: q.QuoteIdx,
      userSignature: 'b7fb597b39fff22256cda44bf1977e21e22d16380d25e13ac1f4a3cf094699305ae61fa58b487c096256c3092cd72aaccd0534e98b009a67237a7efa3fa2839800',
      orderSignature: '67a01cb5ac8eb0ff69ea29c3bfed9198ec5d17285de16d80ec490419ed5994f440c19781dd9acf0c9810f2e17cc4785e176b5f79aaaa1dd92a9dd0f430cf3ad900' });
    return decode({ data: { quoteIdx: q.QuoteIdx, state: 'signed', txId: testQuote.messageSha256 } });
  });
  const signal = new AbortController().signal;
  await expect(authorizeTronDeposit('owner', 'wallet', quote, Date.now()+30000, signal)).rejects.toThrow('LOCAL_VIEW_CLOSED');
  expect(request).toHaveBeenCalledTimes(1);
  expect((await authorizeTronDeposit('owner', 'wallet', testQuote, Date.now()+30000, signal)).state).toBe('signed');
  expect(vault.unprotect).toHaveBeenCalledTimes(2);
});
it('recovers the credited original after expiry without signing again and rejects a credited burn', async () => {
  const status = { quoteIdx: q.QuoteIdx, txId: q.MessageSHA256, state: 'credited', actualFeeSun: '0', actualBandwidth: 266, creditedUsd: '0.01' };
  vi.spyOn(client, 'userRequest').mockImplementation(async (_, decode) => decode({ data: status }));
  const signal = new AbortController().signal;
  expect(await authorizeTronDeposit('owner', 'wallet', quote, 0, signal)).toEqual(status);
  expect(vault.get).not.toHaveBeenCalled();
  status.actualFeeSun = '800';
  await expect(getTronOrder(q.QuoteIdx, signal)).rejects.toThrow('Invalid Nile');
});
