// @vitest-environment node
import { createPublicKey, verify } from 'node:crypto';
import { afterEach, expect, it, vi } from 'vitest';
import type { DepositQuoteData } from '../../packages/api-contract/src/platform';
import proof from '../../../backend/logic/chainreceipt/testdata/ton-self-deposit-credit.json';
import jetton from '../../../backend/logic/chainreceipt/testdata/ton-self-deposit-jetton-repeat.json';
import { authorizeTonDeposit, tonDepositDigests } from '../../apps/user/src/app/native/chain-transfer/ton-deposit';
import { deriveAccounts, importMaterial } from '../../apps/user/src/app/native/local-wallet/crypto';
import * as client from '../../apps/user/src/api/account';
import { mnemonic, tonMnemonic } from './local-wallet-fixture';

vi.mock('../../apps/user/src/api/account', () => ({ userRequest: vi.fn() }));
const vault = vi.hoisted(() => ({ addListener: vi.fn(), protect: vi.fn(), acknowledge: vi.fn(), unprotect: vi.fn(), get: vi.fn() }));
vi.mock('../../apps/user/src/app/native/local-wallet/bridge', () => ({ nativeVault: vault }));
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.clearAllMocks(); vi.unstubAllGlobals(); });
const q = proof.Quote;
const terms: DepositQuoteData = { quoteIdx: q.QuoteIdx, assetId: q.AssetID, gasAssetId: q.GasAssetID, targetUsd: q.TargetUSD,
  estimatedGas: q.EstimatedGas, maxGas: q.MaxGas, gasUsd: q.GasUSD, usdPerCoin: q.USDPerCoin, netUsd: q.NetUSD, payQuantity: q.PayQuantity,
  walletFeePreview: true, walletAddress: q.WalletAddress, tonRecipient: q.Recipient, tonAuthorizationHash: q.TON.AuthorizationHash,
  tonValidUntil: q.TON.ValidUntil, tonUserSeqno: q.TON.UserSeqno, tonSimulatedGas: q.TON.SimulatedGas, relayAttached: q.RelayAttached,
  asOf: q.AsOf, quotedAt: q.QuotedAt, expiresAt: q.ExpiresAt, rateVersion: q.Version, canSubmit: false };

it('rebuilds the Testnet W5 payload and checks both actual signatures against the confirmed payer', () => {
  const digests = tonDepositDigests(terms);
  const pub = createPublicKey({ key: Buffer.from('302a300506032b6570032100' + q.TON.UserKey, 'hex'), type: 'spki', format: 'der' });
  expect(verify(null, digests.user, pub, Buffer.from(proof.UserSignature, 'hex'))).toBe(true);
  expect(verify(null, digests.order, pub, Buffer.from(proof.OrderSignature, 'hex'))).toBe(true);
  for (const change of [{ payQuantity: '0.02' }, { relayAttached: '0.01' }, { tonRecipient: q.WalletAddress },
    { tonUserSeqno: q.TON.UserSeqno + 1 }, { tonValidUntil: q.TON.ValidUntil + 1 }]) {
    expect(() => tonDepositDigests({ ...terms, ...change })).toThrow();
  }
  const other = tonDepositDigests({ ...terms, quoteIdx: '0'.repeat(32) });
  expect(verify(null, other.order, pub, Buffer.from(proof.OrderSignature, 'hex'))).toBe(false);
});

it.each(['bip39', 'ton'] as const)('authorizes only the selected %s W5 and resumes a credited order without unlocking again', async scheme => {
  vi.stubGlobal('document', { visibilityState: 'visible' });
  vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(q.QuotedAt);
  const material = await importMaterial(scheme === 'bip39' ? mnemonic : tonMnemonic, scheme, 'v5r1');
  const wallet = (await deriveAccounts(material)).find(a => a.network === 'ton-testnet')!;
  vault.protect.mockResolvedValue({ active: true, revision: 1 }); vault.addListener.mockResolvedValue({ remove: vi.fn() });
  vault.get.mockResolvedValue({ secret: JSON.stringify(material) });
  let credited = false;
  vi.mocked(client.userRequest).mockImplementation(async (path, decode, opts) => {
    if (path.endsWith('/authorize')) {
      expect(opts?.body).toMatchObject({ quoteIdx: q.QuoteIdx, userSignature: expect.stringMatching(/^[a-f0-9]{128}$/), orderSignature: expect.stringMatching(/^[a-f0-9]{128}$/) });
      return decode({ data: { state: 'accepted', quoteIdx: q.QuoteIdx } });
    }
    return decode({ data: credited ? { quoteIdx: q.QuoteIdx, state: 'credited', dispatchBodyHash: 'a'.repeat(64), receiveTxHash: 'b'.repeat(64), actualFeeNano: '1163296', creditedUsd: '0.01' } : { quoteIdx: q.QuoteIdx, state: 'awaiting_signature' } });
  });
  const signal = new AbortController().signal;
  await expect(authorizeTonDeposit('user','local',terms,q.ExpiresAt,signal)).rejects.toThrow('LOCAL_VIEW_CLOSED');
  const chosen = { ...terms, walletAddress: wallet.address };
  expect((await authorizeTonDeposit('user','local',chosen,q.ExpiresAt,signal)).state).toBe('accepted');
  credited = true;
  expect((await authorizeTonDeposit('user','local',chosen,0,signal)).state).toBe('credited');
  expect(vault.get).toHaveBeenCalledTimes(2);
});

it('rebuilds the signed test USDT transfer, the refund destination and the independently bound order', () => {
  const quote = jetton.Quote;
  const input: DepositQuoteData = { ...terms, assetId: quote.AssetID, quoteIdx: quote.QuoteIdx, walletAddress: quote.WalletAddress,
    tonRecipient: quote.Recipient, payQuantity: quote.PayQuantity, estimatedGas: quote.EstimatedGas, maxGas: quote.MaxGas,
    tonValidUntil: quote.TON.ValidUntil, tonUserSeqno: quote.TON.UserSeqno, tonJettonWalletCode: quote.TON.JettonWalletCode,
    tonAuthorizationHash: quote.TON.AuthorizationHash, relayAttached: quote.RelayAttached, expiresAt: quote.ExpiresAt };
  const pub = createPublicKey({ key: Buffer.from('302a300506032b6570032100' + quote.TON.UserKey, 'hex'), type: 'spki', format: 'der' });
  const d = tonDepositDigests(input);
  expect(verify(null, d.user, pub, Buffer.from(jetton.UserSignature, 'hex'))).toBe(true);
  expect(verify(null, d.order, pub, Buffer.from(jetton.OrderSignature, 'hex'))).toBe(true);
  for (const change of [{ payQuantity: '0.017444' }, { tonRecipient: '0:' + 'a'.repeat(64) }, { relayAttached: '0.003' },
    { tonJettonWalletCode: '00' }, { assetId: 'ton-testnet:usdt:' + '0'.repeat(64) }]) {
    expect(() => tonDepositDigests({ ...input, ...change })).toThrow();
  }
});
