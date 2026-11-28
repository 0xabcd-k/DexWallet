import { afterEach, expect, it, vi } from 'vitest';
import nativeFixture from '../../../backend/logic/chainreceipt/testdata/base-eth-self-deposit.json';
import fixture from '../../../backend/logic/chainreceipt/testdata/base-usdc-self-deposit.json';
import type { DepositQuoteData } from '../../packages/api-contract/src/platform';
import { authorizeBaseDeposit, baseDepositDigest, getBaseOrder } from '../../apps/user/src/app/native/chain-transfer/base-deposit';
import * as client from '../../apps/user/src/api/account';
import { mnemonic } from './local-wallet-fixture';

const vault = vi.hoisted(() => ({ addListener: vi.fn(), protect: vi.fn(), acknowledge: vi.fn(), unprotect: vi.fn(), get: vi.fn() }));
vi.mock('../../apps/user/src/app/native/local-wallet/bridge', () => ({ nativeVault: vault }));
afterEach(() => { vi.restoreAllMocks(); vi.clearAllMocks(); });
const f = fixture.quote;
const quote: DepositQuoteData = {
  assetId: f.AssetID, gasAssetId: f.GasAssetID, walletAddress: f.WalletAddress, baseRecipient: f.Recipient,
  baseFeePayer: f.FeePayer, baseValidBefore: f.BaseValidBefore, maxGas: f.MaxGas, estimatedGas: f.EstimatedGas,
  quoteIdx: f.QuoteIdx, walletFeePreview: true, targetUsd: f.TargetUSD, netUsd: f.NetUSD, payQuantity: f.PayQuantity,
  usdPerCoin: f.USDPerCoin, gasUsd: f.GasUSD, quotedAt: f.QuotedAt, asOf: f.AsOf, expiresAt: f.ExpiresAt, rateVersion: f.Version, canSubmit: false,
};

it('binds the domain, source, destination, gross quantity, authorization expiry and order nonce', () => {
  expect('0x' + Buffer.from(baseDepositDigest(quote)).toString('hex')).toBe(fixture.digest);
  for (const changes of [{ baseRecipient: '0x' + '1'.repeat(40) }, { quoteIdx: '2'.repeat(32) },
    { walletAddress: '0x' + '2'.repeat(40) }, { payQuantity: '0.011788' }, { baseValidBefore: f.BaseValidBefore + 1 }]) {
    expect(baseDepositDigest({ ...quote, ...changes })).not.toEqual(baseDepositDigest(quote));
  }
  for (const changes of [{ assetId: 'base:usdc' }, { gasAssetId: 'base:eth' }, { maxGas: '0' },
    { baseValidBefore: Math.floor(f.ExpiresAt / 1000) }, { payQuantity: '0.0000001' }, { baseRecipient: f.WalletAddress }]) {
    expect(() => baseDepositDigest({ ...quote, ...changes })).toThrow();
  }
});

const n = nativeFixture.quote;
const nativeQuote: DepositQuoteData = { ...quote, assetId: n.AssetID, quoteIdx: n.QuoteIdx, walletAddress: n.WalletAddress,
  baseRecipient: n.Recipient, baseFeePayer: n.FeePayer, payQuantity: n.PayQuantity, baseValidBefore: n.BaseValidBefore,
  maxGas: n.MaxGas, estimatedGas: n.EstimatedGas, quotedAt: n.QuotedAt, expiresAt: n.ExpiresAt };
it('matches the native redemption digest and binds its allowance, order, dates and payee', () => {
  const expected = Buffer.from(baseDepositDigest(nativeQuote)).toString('hex');
  expect('0x' + expected).toBe(nativeFixture.digest);
  for (const change of [{ payQuantity: '0.00002' }, { quoteIdx: 'f'.repeat(32) }, { quotedAt: n.QuotedAt - 2000 },
    { baseValidBefore: n.BaseValidBefore + 1 }, { baseRecipient: '0x' + '1'.repeat(40), baseFeePayer: '0x' + '1'.repeat(40) }]) {
    expect(Buffer.from(baseDepositDigest({ ...nativeQuote, ...change })).toString('hex')).not.toBe(expected);
  }
  expect(() => baseDepositDigest({ ...nativeQuote, baseFeePayer: '0x' + '1'.repeat(40) })).toThrow();
});

it.each([
  { terms: nativeQuote, signature: '0x1ec7c79b95ff1cccc2e50b1c163d991a912101b4fb54245b44f30bc6f0f097396e25bc38b5701443592ab5f128f58740a1ec874d88307b67884eab4c2a33617c1b' },
  { terms: quote, signature: '0xe1aa297a7f04fd94ae78fae7a065d95037da305f7c7f69afcb2d2f710c4bcf2f6156f16f3e79f0f5572f1f2234864a13ecd4fc27417a68bc03002ecd9e6a8ca91b' },
])('signs the independent $terms.assetId test vector after device unlock', async ({ terms, signature }) => {
  const idx = terms.quoteIdx!;
  const signal = new AbortController().signal;
  vault.protect.mockResolvedValue({ active: true, revision: 1 });
  vault.addListener.mockResolvedValue({ remove: vi.fn() });
  vault.get.mockResolvedValue({ secret: JSON.stringify({ version: 1, scheme: 'bip39', tonVersion: 'v5r1', words: mnemonic }) });
  const api = vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode, options) => {
    if (path.endsWith(idx)) return decode({ data: { quoteIdx: idx, assetId: terms.assetId, state: 'awaiting_signature' } });
    expect(path).toBe('/v1/user/base-deposit-orders/authorize');
    expect(options?.body).toEqual({ quoteIdx: idx, userSignature: signature });
    return decode({ data: { quoteIdx: idx, assetId: terms.assetId, state: 'accepted' } });
  });
  const deadline = Date.now() + 30000;
  await expect(authorizeBaseDeposit('user','wallet',terms,deadline,signal)).rejects.toThrow('LOCAL_VIEW_CLOSED');
  expect(api).toHaveBeenCalledTimes(1);
  const q = { ...terms, walletAddress: '0x9858EfFD232B4033E47d90003D41EC34EcaEda94' };
  expect((await authorizeBaseDeposit('user','wallet',q,deadline,signal)).state).toBe('accepted');
  expect(vault.unprotect).toHaveBeenCalledTimes(2);
});

it('recovers an accepted or credited order after expiry without unlocking the wallet', async () => {
  const idx = f.QuoteIdx, signal = new AbortController().signal;
  const data = { assetId: 'base-sepolia:usdc', quoteIdx: idx, state: 'credited', txHash: fixture.evidence.transaction.hash, actualFeeWei: '558417902115', creditedUsd: '0.01' };
  vi.spyOn(client, 'userRequest').mockImplementation(async (_,decode) => decode({ data }));
  expect(await authorizeBaseDeposit('user','wallet',quote,0,signal)).toEqual(data);
  data.creditedUsd = '0.00';
  await expect(getBaseOrder(idx,signal)).rejects.toThrow();
  expect(vault.get).not.toHaveBeenCalled();
});
