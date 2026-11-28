import jettonDeposit from '../../../backend/logic/chainreceipt/testdata/ton-self-deposit-jetton-repeat.json';
import { MemoryRouter } from 'react-router';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChainNetwork, DepositQuoteData } from '../../packages/api-contract/src/platform';
import * as client from '../../apps/user/src/api/account';
import * as catalog from '../../apps/user/src/app/native/chain-assets/api';
import * as walletSource from '../../apps/user/src/app/native/chain-assets/use-wallets';
import { previewDeposit } from '../../apps/user/src/app/native/chain-transfer/deposit';
import DepositButton, { BeneficiaryDeposit, DepositEstimate } from '../../apps/user/src/view/chain-transfer/deposit';
import { LastBaseDeposit } from '../../apps/user/src/view/chain-transfer/base-deposit';
import { LastSolDeposit } from '../../apps/user/src/view/chain-transfer/sol-deposit';
import { nativeCatalog } from '../../apps/user/src/app/native/local-wallet/bridge';
import { appResources } from '../../apps/user/src/locales';

vi.mock('../../apps/user/src/app/native/local-wallet/bridge', async (load) => ({
  ...await load<typeof import('../../apps/user/src/app/native/local-wallet/bridge')>(),
  nativeCatalog: { get: vi.fn().mockResolvedValue({ value: null }) },
}));

const network: ChainNetwork = { id: 'base-sepolia', family: 'base', enabled: false, testnet: true, assets: [
  { id: 'base-sepolia:eth', symbol: 'ETH', contract: '', decimals: 18, origin: 'native', depositPreview: true },
  { id: 'base-sepolia:usdc', symbol: 'USDC', contract: 'test-usdc', decimals: 6, origin: 'issuer', depositPreview: true },
] };
const quote = (now = Date.now()): DepositQuoteData => ({ assetId: 'base-sepolia:usdc', targetUsd: '10.00', usdPerCoin: '0.98',
  payQuantity: '10.408164', gasAssetId: 'base-sepolia:eth', estimatedGas: '0.0001', gasUsd: '0.2', netUsd: '10.00',
  rateVersion: 'test-1', rateSource: 'kraken', asOf: now - 10000, quotedAt: now, expiresAt: now + 30000, canSubmit: false });
afterEach(() => vi.restoreAllMocks());
async function open() {
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<I18nextProvider i18n={i18n}><DepositButton userIdx="uid123"/></I18nextProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Deposit' }));
}
it('shows only quoted test assets, ties results to the current input and expires a displayed estimate', async () => {
  vi.spyOn(catalog, 'getCatalog').mockResolvedValue([network]);
  let finish!: () => void;
  const request = vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode) => {
    await new Promise<void>(done => { finish = done; });
    return decode({ data: quote() });
  });
  await open();
  const asset = await screen.findByLabelText('Asset');
  fireEvent.change(asset, { target: { value: 'base-sepolia:usdc' } });
  const amount = screen.getByLabelText('Target USD credit ($)');
  const submit = () => screen.getByRole('button', { name: 'Get estimate' });
  expect(submit()).toBeDisabled();
  fireEvent.change(amount, { target: { value: '10' } });
  fireEvent.click(submit());
  expect(request.mock.calls[0]?.[2]).toMatchObject({ body: { assetId: 'base-sepolia:usdc', targetUsd: '10' } });
  fireEvent.change(amount, { target: { value: '12' } });
  await act(async () => { finish(); });
  expect(screen.queryByText('10.408164 USDC')).toBeNull();
  fireEvent.change(amount, { target: { value: '10' } });
  fireEvent.click(submit()); await act(async () => { finish(); });
  expect(await screen.findByText('10.408164 USDC')).toBeInTheDocument();
  expect(screen.getByText('0.0001 ETH · $0.2')).toBeInTheDocument();
  expect(screen.getByText('$10.00')).toBeInTheDocument();
  expect(screen.getByText(/Kraken spot market/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Confirm payment' })).toBeNull();
  const later = Date.now() + 31000;
  vi.spyOn(Date, 'now').mockReturnValue(later);
  act(() => { document.dispatchEvent(new Event('visibilitychange')); });
  expect(await screen.findByText('Estimate expired. Request a new estimate.')).toBeInTheDocument();
  expect(screen.queryByText('10.408164 USDC')).toBeNull();
});
it('shows a retryable empty state when the server has no current test sheet', async () => {
  const list = vi.spyOn(catalog, 'getCatalog').mockResolvedValue([]);
  await open();
  expect(await screen.findByText('No current rates are available for a testnet deposit.')).toBeInTheDocument();
  expect(screen.queryByRole('combobox')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
});

it('prices only the selected Solana wallet and discards the result on wallet change', async () => {
  const sol = { id: 'solana-devnet:sol', symbol: 'SOL', contract: '', decimals: 9, origin: 'native' as const, depositPreview: true };
  vi.spyOn(catalog, 'getCatalog').mockResolvedValue([{ id: 'solana-devnet', family: 'solana', enabled: true, testnet: true, assets: [sol] }]);
  const a = '9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf', b = '7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5';
  vi.spyOn(walletSource, 'useViewingWallets').mockReturnValue({
    wallets: [a, b].map((address, i) => ({ id: address, name: 'Wallet ' + (i + 1), source: 'local', accounts: [{ network: 'solana', address }] })),
    loading: false, error: undefined, connectionError: false, refresh: vi.fn(),
  });
  const request = vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode, options) => {
    const walletAddress = (options?.body as { walletAddress?: string }).walletAddress;
    const now = Date.now();
    return decode({ data: { ...quote(), assetId: sol.id, gasAssetId: sol.id, usdPerCoin: '100', walletFeePreview: true,
      quotedAt: now, expiresAt: now + 30000, payQuantity: walletAddress ? '0.100012345' : '0.101',
      estimatedGas: walletAddress ? '0.000012345' : '0.001', gasUsd: walletAddress ? '0.0012345' : '0.1',
      ...(walletAddress ? { walletAddress: a, messageSha256: 'a'.repeat(64) } : {}),
    } });
  });
  await open();
  fireEvent.change(await screen.findByLabelText('Target USD credit ($)'), { target: { value: '10' } });
  fireEvent.click(screen.getByRole('button', { name: 'Get estimate' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Choose payment wallet' }));
  expect(screen.queryByText('0.101 SOL')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: /Wallet 1/ }));
  const getFee = () => screen.getByRole('button', { name: 'Get network fee for this wallet' });
  fireEvent.click(getFee());
  expect(await screen.findByText('0.100012345 SOL')).toBeInTheDocument();
  expect(request.mock.calls[1]?.[2]).toMatchObject({ body: { walletAddress: a } });
  fireEvent.click(screen.getByRole('button', { name: /Wallet 2/ }));
  expect(screen.queryByText('0.100012345 SOL')).toBeNull();
  fireEvent.click(getFee());
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  expect(request.mock.calls[2]?.[2]).toMatchObject({ body: { walletAddress: b } });
  expect(screen.queryByText('0.100012345 SOL')).toBeNull();
});

const testMint = '2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5';
const usdc = { id: 'solana-devnet:usdc', symbol: 'USDC', contract: '4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU', rate: '1', pay: '0.011' };
const usdt = { id: 'solana-devnet:usdt:' + testMint, symbol: 'USDT', contract: testMint, rate: '0.98', pay: '0.011225' };
it.each([
  { ...usdc, funding: '0', usd: '0' }, { ...usdc, funding: '0.00148844', usd: '0.148844' },
  { ...usdt, funding: '0', usd: '0' }, { ...usdt, funding: '0.00148844', usd: '0.148844' },
])('shows $funding SOL of platform account funding separately from the $symbol total', async ({ symbol, id, contract, rate, pay, funding, usd }) => {
  const native = { ...network.assets[0]!, id: 'solana-devnet:sol', symbol: 'SOL', decimals: 9 };
  const asset = { ...network.assets[1]!, id, symbol, contract, origin: symbol === 'USDT' ? 'test_fixture' as const : 'issuer' as const };
  const wallet = '9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf';
  const result = { ...quote(), assetId: asset.id, gasAssetId: native.id, usdPerCoin: rate, targetUsd: '0.01', netUsd: '0.01',
    estimatedGas: '0.00001', gasUsd: '0.001', payQuantity: pay, walletAddress: wallet, walletFeePreview: true, messageSha256: 'a'.repeat(64) };
  vi.spyOn(client, 'userRequest').mockImplementationOnce(async (_path, decode) =>
    decode({ data: { ...result, accountFunding: funding, accountFundingUsd: usd, quoteIdx: '1'.repeat(32),
      maxGas: result.estimatedGas, solOrderIdx: '2'.repeat(32), solRecipient: '5ZAhXYgaYEZ9UgH8KKFdncPjA4NBq2c1VbefvBd3Dgif',
      solFeePayer: 'DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN' } })).mockImplementationOnce(async (_path, decode) =>
    decode({ data: result })).mockImplementationOnce(async (_path, decode) =>
    decode({ data: { ...result, accountFunding: funding, accountFundingUsd: usd, assetId: id + '-other' } }));
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<I18nextProvider i18n={i18n}><DepositEstimate asset={asset} native={native} target="0.01" walletAddress={wallet}/></I18nextProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText(`${funding} SOL · $${usd}`)).toBeInTheDocument();
  expect(screen.getByText('0.00001 SOL · $0.001')).toBeInTheDocument();
  expect(screen.getByText(`1 ${symbol} = $${rate}`)).toBeInTheDocument();
  expect(screen.getByText(`${pay} ${symbol}`)).toBeInTheDocument();
  const retry = () => previewDeposit(asset, native, '0.01', new AbortController().signal, wallet);
  await expect(retry()).rejects.toThrow('Invalid test deposit estimate'); // Missing account funding
  await expect(retry()).rejects.toThrow('Invalid test deposit estimate'); // Another asset's quote
});

it.each([
  { asset: network.assets[0]!, pay: '0.0051', rate: '2000' },
  { asset: network.assets[1]!, pay: '10.408164', rate: '0.98' },
])('shows the selected Base $asset.symbol pre-signing fee and requires a fee block', async ({ asset, pay, rate }) => {
  const response = () => ({ ...quote(), assetId: asset.id, usdPerCoin: rate, payQuantity: pay, walletFeePreview: true });
  const wallet = '0xecd14F7586f5053CECae148Ae26309C2277D86A8';
  vi.spyOn(catalog, 'getCatalog').mockResolvedValue([{ ...network, enabled: true }]);
  vi.spyOn(walletSource, 'useViewingWallets').mockReturnValue({
    wallets: [{ id: 'local-base', name: 'Base wallet', source: 'local', accounts: [{ network: 'base', address: wallet }] }],
    loading: false, error: undefined, connectionError: false, refresh: vi.fn(),
  });
  const block = '0x' + 'b'.repeat(64);
  const request = vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode, options) => {
    const { walletAddress } = options?.body as { walletAddress?: string };
    return decode({ data: { ...response(), ...(walletAddress ? { walletAddress, feeBlockHash: block } : {}) } });
  });
  await open();
  fireEvent.change(await screen.findByLabelText('Asset'), { target: { value: asset.id } });
  fireEvent.change(screen.getByLabelText('Target USD credit ($)'), { target: { value: '10' } });
  fireEvent.click(screen.getByRole('button', { name: 'Get estimate' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Choose payment wallet' }));
  expect(!!screen.queryByText(/account already upgraded/)).toBe(asset.symbol === 'ETH');
  fireEvent.click(screen.getByRole('button', { name: /Base wallet/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText('Estimated network fee before signing (included in total)')).toBeInTheDocument();
  expect(screen.getByText(`${pay} ${asset.symbol}`)).toBeInTheDocument();
  expect(request.mock.calls[1]?.[2]).toMatchObject({ body: { assetId: asset.id, walletAddress: wallet } });
  expect(screen.queryByRole('button', { name: 'Confirm payment' })).toBeNull();
  request.mockImplementationOnce(async (_path, decode) => decode({ data: {
    ...response(), walletAddress: wallet, messageSha256: 'a'.repeat(64),
  } }));
  await expect(previewDeposit(asset, network.assets[0]!, '10', new AbortController().signal, wallet)).rejects.toThrow('Invalid test deposit estimate');
});

it.each(['TRX', 'USDT'])('validates delegated Nile resources in the %s wallet estimate', async symbol => {
  const trx = { id: 'tron-nile:trx', symbol: 'TRX', origin: 'native' as const, contract: '', decimals: 6, depositPreview: true };
  const contract = 'TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn';
  const token = { ...trx, id: 'tron-nile:usdt:' + contract, symbol: 'USDT', origin: 'test_fixture' as const, contract };
  const isToken = symbol === 'USDT';
  const asset = isToken ? token : trx;
  const wallet = 'TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio';
  vi.spyOn(catalog, 'getCatalog').mockResolvedValue([{ id: 'tron-nile', family: 'tron', enabled: true, testnet: true, assets: [trx, token] }]);
  vi.spyOn(walletSource, 'useViewingWallets').mockReturnValue({
    wallets: [{ id: 'nile', name: 'Nile wallet', source: 'local', accounts: [{ network: 'tron', address: wallet }] }],
    loading: false, error: undefined, connectionError: false, refresh: vi.fn(),
  });
  const estimate = () => ({ ...quote(), assetId: asset.id, gasAssetId: trx.id, rateSource: 'test_sheet', targetUsd: '0.01', usdPerCoin: isToken ? '1' : '10', payQuantity: isToken ? '0.01' : '0.001',
    netUsd: '0.01', estimatedGas: '0', gasUsd: '0', walletFeePreview: true });
  const evidence = { walletAddress: wallet, messageSha256: 'a'.repeat(64), delegatedBandwidth: isToken ? 345 : 266, ...(isToken ? { delegatedEnergy: 14650 } : {}) };
  const request = vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode, options) => {
    const { walletAddress } = options?.body as { walletAddress?: string };
    return decode({ data: { ...estimate(), ...(walletAddress ? evidence : {}) } });
  });
  await open();
  fireEvent.change(await screen.findByLabelText('Asset'), { target: { value: asset.id } });
  fireEvent.change(await screen.findByLabelText('Target USD credit ($)'), { target: { value: '0.01' } });
  fireEvent.click(screen.getByRole('button', { name: 'Get estimate' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Choose payment wallet' }));
  fireEvent.click(screen.getByRole('button', { name: /Nile wallet/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText('Estimated platform-delegated bandwidth (points)')).toBeInTheDocument();
  expect(screen.getByText(String(evidence.delegatedBandwidth))).toBeInTheDocument();
  expect(screen.getByText('0 TRX · $0')).toBeInTheDocument();
  expect(screen.getByText(isToken ? '0.01 USDT' : '0.001 TRX')).toBeInTheDocument();
  expect(!!screen.queryByText('Estimated platform-delegated Energy (units)')).toBe(isToken);
  if (isToken) expect(screen.getByText('14650')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Confirm payment' })).toBeNull();
  for (const change of [{ delegatedBandwidth: undefined }, { delegatedBandwidth: 0 }, { estimatedGas: '1' }, { messageSha256: '' }, { delegatedEnergy: isToken ? undefined : 100 }, { delegatedEnergy: 0 }]) {
    request.mockImplementationOnce(async (_path, decode) => decode({ data: { ...estimate(), ...evidence, ...change } }));
    await expect(previewDeposit(asset, trx, '0.01', new AbortController().signal, wallet)).rejects.toThrow('Invalid test deposit estimate');
  }
});

it('shows the W5 relay advance and remainder separately from the fee included in the TON payment', async () => {
  const ton = { id: 'ton-testnet:ton', symbol: 'TON', origin: 'native' as const, contract: '', decimals: 9, depositPreview: true };
  const wallet = '0:a393a594688a53f6138d88f3447a8991093cc387f0463d0acd955289fadbef7a';
  vi.spyOn(catalog, 'getCatalog').mockResolvedValue([{ id: 'ton-testnet', family: 'ton', enabled: true, testnet: true, assets: [ton] }]);
  vi.spyOn(walletSource, 'useViewingWallets').mockReturnValue({
    wallets: [{ id: 'w5', name: 'Tonkeeper testnet', source: 'external', accounts: [{ network: 'ton-testnet', address: wallet }] }],
    loading: false, error: undefined, connectionError: false, refresh: vi.fn(),
  });
  const base = { ...quote(), assetId: ton.id, gasAssetId: ton.id, targetUsd: '0.01', netUsd: '0.01', usdPerCoin: '1',
    payQuantity: '0.011246787', estimatedGas: '0.001246787', gasUsd: '0.001246787', walletFeePreview: true };
  const evidence = { walletAddress: wallet, emulatedMessageHash: 'a'.repeat(64), relayAttached: '0.003', relaySurplus: '0.002500974' };
  const request = vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode, options) =>
    decode({ data: { ...base, ...((options?.body as { walletAddress?: string }).walletAddress ? evidence : {}) } }));
  await open();
  fireEvent.change(await screen.findByLabelText('Target USD credit ($)'), { target: { value: '0.01' } });
  fireEvent.click(screen.getByRole('button', { name: 'Get estimate' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Choose payment wallet' }));
  fireEvent.click(screen.getByRole('button', { name: /Tonkeeper testnet/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText('0.011246787 TON')).toBeInTheDocument();
  expect(screen.getByText('0.001246787 TON · $0.001246787')).toBeInTheDocument();
  expect(screen.getByText('0.003 TON')).toBeInTheDocument();
  expect(screen.getByText('0.002500974 TON')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Confirm payment' })).toBeNull();
  for (const change of [{ emulatedMessageHash: '' }, { relayAttached: undefined }, { relaySurplus: '0.004' }, { estimatedGas: '0' }, { messageSha256: 'b'.repeat(64) }]) {
    request.mockImplementationOnce(async (_path, decode) => decode({ data: { ...base, ...evidence, ...change } }));
    await expect(previewDeposit(ton, ton, '0.01', new AbortController().signal, wallet)).rejects.toThrow('Invalid test deposit estimate');
  }
});

it('quotes TON test USDT in token units and keeps refund and wallet reserve outside the network fee', async () => {
  const ton = { id: 'ton-testnet:ton', symbol: 'TON', origin: 'native' as const, contract: '', decimals: 9, depositPreview: true };
  const master = '0:f418a04cf196ebc959366844a6cdf53a6fd6fff1eadafc892f05210bba31593e';
  const asset = { id: 'ton-testnet:usdt:' + master, symbol: 'USDT', origin: 'test_fixture' as const, contract: master, decimals: 6, depositPreview: true };
  const wallet = '0:a393a594688a53f6138d88f3447a8991093cc387f0463d0acd955289fadbef7a';
  const result = { ...quote(), assetId: asset.id, gasAssetId: ton.id, targetUsd: '0.01', netUsd: '0.01', usdPerCoin: '0.98',
    payQuantity: '0.017745', estimatedGas: '0.003694994', gasUsd: '0.007389988', walletFeePreview: true, walletAddress: wallet,
    emulatedMessageHash: 'a'.repeat(64), relayAttached: '0.053', relaySurplus: '0.002449983', jettonExcess: '0.047774744',
    accountFunding: '0.000006283', accountFundingUsd: '0.000012566' };
  const request = vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode) => decode({ data: result }));
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<I18nextProvider i18n={i18n}><DepositEstimate asset={asset} native={ton} target="0.01" walletAddress={wallet}/></I18nextProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText('0.017745 USDT')).toBeInTheDocument();
  for (const label of ['0.003694994 TON · $0.007389988', '0.047774744 TON', '0.000006283 TON · $0.000012566', '0.002449983 TON']) {
    expect(screen.getByText(label)).toBeInTheDocument();
  }
  for (const change of [{ jettonExcess: undefined }, { jettonExcess: '0' }, { accountFunding: undefined }, { gasAssetId: asset.id }]) {
    request.mockImplementationOnce(async (_path, decode) => decode({ data: { ...result, ...change } }));
    await expect(previewDeposit(asset, ton, '0.01', new AbortController().signal, wallet)).rejects.toThrow('Invalid test deposit estimate');
  }
});

it('recovers a saved wallet quote by its original idx when the response is lost and the offer expires', async () => {
  const wallet = '0xecd14F7586f5053CECae148Ae26309C2277D86A8';
  const terms = { ...quote(), walletAddress: wallet, walletFeePreview: true, feeBlockHash: '0x' + 'a'.repeat(64),
    quoteIdx: '1'.repeat(32), maxGas: '0.0001' };
  const request = vi.spyOn(client, 'userRequest')
    .mockImplementationOnce(async (_path, decode) => decode({ data: terms }))
    .mockRejectedValueOnce(new Error('connection lost after confirmation'))
    .mockImplementationOnce(async (_path, decode) => decode({ data: { quote: terms, confirmedAt: terms.quotedAt + 1 } }));
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<I18nextProvider i18n={i18n}><DepositEstimate asset={network.assets[1]!} native={network.assets[0]!} target="10" walletAddress={wallet}/></I18nextProvider>);
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText('Maximum network fee included in this quote')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Confirm quote' }));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  expect(request.mock.calls[1]?.[0]).toBe('/v1/user/deposit-quotes/confirm');
  expect(request.mock.calls[1]?.[2]?.body).toEqual({ quoteIdx: terms.quoteIdx });
  vi.spyOn(Date, 'now').mockReturnValue(terms.expiresAt + 1000);
  act(() => { document.dispatchEvent(new Event('visibilitychange')); });
  fireEvent.click(await screen.findByRole('button', { name: 'Check confirmation' }));
  expect(await screen.findByText(new RegExp(terms.quoteIdx))).toBeInTheDocument();
  expect(request.mock.calls[2]?.[0]).toBe('/v1/user/deposit-quotes/' + terms.quoteIdx);
  expect(request.mock.calls[2]?.[2]?.body).toBeUndefined();
  expect(screen.queryByRole('button', { name: 'Confirm quote' })).toBeNull();
});

it.each([[false, undefined], [true, undefined], [true, '0'], [true, '1488440']] as const)('recovers a finalized deposit and settlement (credited=%s, storage=%s)', async (credited, funding) => {
  const idx = '2'.repeat(32);
  const tx = 'tTrrbqujhmBRtQwWnDNeZfU1SaWBWbPVsCC7dUqAovpBA2t2NtEWkKJYryNpBH6meGE5jLgdk3m8VXnbRbaFBrD';
  vi.mocked(nativeCatalog.get).mockResolvedValueOnce({ value: idx });
  vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode) => decode({ data: {
    quoteIdx: idx, orderIdx: idx, state: 'accepted', executionState: 'finalized', txSignature: tx, actualFeeLamports: 10000,
    ...(funding !== undefined ? { assetId: funding === '0' ? usdc.id : usdt.id, actualFundingLamports: funding } : {}),
    acceptedAt: 10, quoteExpiresAt: 20, reviewAfter: 30, lastValidBlockHeight: 100, messageSha256: 'a'.repeat(64), messageBase64: 'Ag==', ...(credited ? { creditedAt: 31, creditedUsd: '0.01' } : {}),
  } }));
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<MemoryRouter><I18nextProvider i18n={i18n}><LastSolDeposit userIdx="testuser"/></I18nextProvider></MemoryRouter>);
  fireEvent.click(await screen.findByRole('button', { name: 'Check Solana deposit status' }));
  expect(await screen.findByRole('status')).toHaveTextContent(credited ? 'credited 0.01 test USD' : 'confirmed on Devnet. Test USD has not been credited.');
  if (credited) expect(screen.getByRole('link')).toHaveAttribute('href', '/records?kind=usd_balance');
  expect(screen.getByText(tx)).toBeInTheDocument();
  expect(screen.getByText('10000 lamports')).toBeInTheDocument();
  if (funding !== undefined) expect(screen.getByText(funding + ' lamports')).toBeInTheDocument();
});

it('looks up a finalized Base deposit without a wallet and opens the USD statement', async () => {
  const idx = 'a'.repeat(32), txHash = '0x' + 'b'.repeat(64);
  vi.mocked(nativeCatalog.get).mockResolvedValueOnce({ value: idx });
  const api = vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode) => {
    expect(path).toBe('/v1/user/base-deposit-orders/' + idx);
    return decode({ data: { quoteIdx: idx, state: 'credited', txHash, actualFeeWei: '558417902115', creditedUsd: '0.01' } });
  });
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<MemoryRouter><I18nextProvider i18n={i18n}><LastBaseDeposit userIdx="testuser"/></I18nextProvider></MemoryRouter>);
  fireEvent.click(await screen.findByRole('button', { name: 'Check Base deposit status' }));
  expect(await screen.findByText(txHash)).toBeInTheDocument();
  expect(screen.getByText('558417902115 wei')).toBeInTheDocument();
  expect(screen.getByRole('status')).toHaveTextContent('0.01 test USD');
  expect(screen.getByRole('link')).toHaveAttribute('href', '/records?kind=usd_balance');
  expect(api).toHaveBeenCalledOnce();
});

it.each(['USDC','ETH'])('offers Base %s signing after confirming the exact wallet quote', async symbol => {
  const native = network.assets[0]!, asset = symbol === 'ETH' ? native : { ...network.assets[1]!, contract: '0x036CbD53842c5426634e7929541eC2318f3dCF7e' };
  const wallet = '0x' + '1'.repeat(40), sponsor = '0x' + '3'.repeat(40), recipient = symbol === 'ETH' ? sponsor : '0x' + '2'.repeat(40);
  const now = Date.now(), terms = { ...quote(now), assetId: asset.id, quoteIdx: 'd'.repeat(32), walletAddress: wallet, walletFeePreview: true,
    feeBlockHash: '0x' + 'b'.repeat(64), maxGas: '0.0001', baseFeePayer: sponsor, baseRecipient: recipient, baseValidBefore: Math.floor(now / 1000) + 330 };
  vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode) => decode({ data:
    path.endsWith('/confirm') ? { quote: terms, confirmedAt: now + 1 } : terms }));
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<MemoryRouter><I18nextProvider i18n={i18n}><DepositEstimate asset={asset} native={native} target="10"
    walletAddress={wallet} local={{ userIdx: 'testuser', walletId: 'base' }}/></I18nextProvider></MemoryRouter>);
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  const confirm = await screen.findByRole('button', { name: 'Confirm quote' });
  expect(screen.queryByRole('button', { name: `Authorize Base ${symbol} deposit` })).toBeNull();
  fireEvent.click(confirm);
  expect(await screen.findByRole('button', { name: `Authorize Base ${symbol} deposit` })).toBeEnabled();
  expect(screen.getAllByText(sponsor).length).toBeGreaterThan(0);
  expect(screen.getAllByText(recipient).length).toBeGreaterThan(0);
  await expect(previewDeposit({ ...asset, contract: wallet }, native, '10', new AbortController().signal, wallet)).rejects.toThrow('Invalid test deposit estimate');
});

it.each([false,true])('shows Nile signing terms and credited resources (token %s)', async token => {
  const txId = 'a'.repeat(64);
  const trx = { id: 'tron-nile:trx', symbol: 'TRX', origin: 'native' as const, contract: '', decimals: 6 };
  const owner = 'TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH', wallet = 'TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio';
  const contract = 'TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn';
  const asset = token ? { id: 'tron-nile:usdt:' + contract, symbol: 'USDT', origin: 'test_fixture' as const, decimals: 6, contract } : trx;
  const label = token ? 'Test USDT' : 'TRX';
  const terms = { ...quote(), quoteIdx: 'd'.repeat(32), assetId: asset.id, gasAssetId: trx.id, targetUsd: '0.01', netUsd: '0.01',
    usdPerCoin: token ? '1' : '10', payQuantity: token ? '0.01' : '0.001', estimatedGas: '0', maxGas: '0', gasUsd: '0', walletFeePreview: true,
    walletAddress: wallet, messageSha256: txId, delegatedBandwidth: token ? 345 : 266, delegatedEnergy: token ? 14650 : undefined, tronRefBlock: '1'.repeat(64), tronRefTime: Date.now() - 2000,
    tronRecipient: owner, tronResourceOwner: owner };
  vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode) => {
    if (path.endsWith('/preview')) return decode({ data: terms });
    if (path.endsWith('/confirm')) return decode({ data: { quote: terms, confirmedAt: terms.quotedAt + 1 } });
    return decode({ data: { assetId: asset.id, quoteIdx: terms.quoteIdx, txId, actualFeeSun: '0', actualEnergy: terms.delegatedEnergy, actualBandwidth: terms.delegatedBandwidth, state: 'credited', creditedUsd: '0.01' } });
  });
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<MemoryRouter><I18nextProvider i18n={i18n}><DepositEstimate asset={asset} native={trx} target="0.01"
    walletAddress={wallet} local={{ userIdx: 'self', walletId: 'nile' }}/></I18nextProvider></MemoryRouter>);
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(screen.queryByRole('button', { name: `Authorize ${label} deposit` })).toBeNull();
  fireEvent.click(await screen.findByRole('button', { name: 'Confirm quote' }));
  expect(await screen.findByRole('button', { name: `Authorize ${label} deposit` })).toBeEnabled();
  expect(screen.getAllByText(owner)).toHaveLength(2);
  if (token) { expect(screen.getByText(contract)).toBeInTheDocument(); expect(screen.getByText('15 TRX')).toBeInTheDocument(); }
  else expect(screen.getByText(/TRON may burn TRX from your wallet/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: `Check ${label} deposit status` }));
  expect(await screen.findByText(txId)).toBeInTheDocument();
  expect(screen.getByText('0 sun')).toBeInTheDocument();
  expect(screen.getByRole('link')).toHaveAttribute('href', '/records?kind=usd_balance');
});

it('shows the W5 fee limit before confirmation and the final trace fee on the credited order', async () => {
  const asset = { id: 'ton-testnet:ton', symbol: 'TON', origin: 'native' as const, contract: '', decimals: 9 };
  const wallet = '0:' + '1'.repeat(64), receiveTxHash = 'f'.repeat(64);
  const now = Date.now();
  const terms = { ...quote(now), assetId: asset.id, gasAssetId: asset.id, quoteIdx: 'a'.repeat(32), targetUsd: '0.01', netUsd: '0.01',
    usdPerCoin: '1', payQuantity: '0.011163323', estimatedGas: '0.001163323', maxGas: '0.001163323', gasUsd: '0.001163323',
    walletAddress: wallet, walletFeePreview: true, emulatedMessageHash: 'b'.repeat(64), tonRecipient: '0:' + '2'.repeat(64),
    tonAuthorizationHash: 'c'.repeat(64), tonUserSeqno: 2, tonValidUntil: Math.floor(now / 1000) + 300, tonSimulatedGas: '0.001163294',
    relayAttached: '0.003', relaySurplus: '0.002577588' };
  vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode) => {
    if (path.endsWith('/preview')) return decode({ data: terms });
    if (path.endsWith('/confirm')) return decode({ data: { quote: terms, confirmedAt: now + 1 } });
    expect(path).toBe('/v1/user/ton-deposit-orders/' + terms.quoteIdx);
    return decode({ data: { quoteIdx: terms.quoteIdx, state: 'credited', dispatchBodyHash: 'd'.repeat(64), receiveTxHash,
      actualFeeNano: '1163296', creditedUsd: '0.01' } });
  });
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<MemoryRouter><I18nextProvider i18n={i18n}><DepositEstimate asset={asset} native={asset} target="0.01"
    walletAddress={wallet} local={{ userIdx: 'self', walletId: 'w5' }}/></I18nextProvider></MemoryRouter>);
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText('Maximum network fee including storage until expiry')).toBeInTheDocument();
  expect(screen.getByText('0.001163323 TON · $0.001163323')).toBeInTheDocument();
  expect(screen.getByText('0.001163294 TON')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Authorize TON deposit' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Confirm quote' }));
  expect(await screen.findByRole('button', { name: 'Authorize TON deposit' })).toBeEnabled();
  expect(screen.getByText(terms.tonRecipient)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Check TON deposit' }));
  expect(await screen.findByText(receiveTxHash)).toBeInTheDocument();
  expect(screen.getByText('1163296 nanoTON')).toBeInTheDocument();
  expect(screen.getByRole('link')).toHaveAttribute('href', '/records?kind=usd_balance');
  expect(screen.queryByRole('button', { name: 'Authorize TON deposit' })).toBeNull();
});

it('confirms a test Jetton quote and shows the existing wallet fee separately from the recoverable fee', async () => {
  const q = jettonDeposit.Quote, now = Date.now();
  const native = { id: q.GasAssetID, symbol: 'TON', origin: 'native' as const, contract: '', decimals: 9 };
  const asset = { id: q.AssetID, symbol: 'USDT', origin: 'test_fixture' as const, contract: q.AssetID.slice('ton-testnet:usdt:'.length), decimals: 6 };
  const terms = { ...quote(now), assetId: asset.id, gasAssetId: native.id, quoteIdx: q.QuoteIdx, targetUsd: q.TargetUSD, netUsd: q.NetUSD,
    usdPerCoin: q.USDPerCoin, payQuantity: q.PayQuantity, gasUsd: q.GasUSD, estimatedGas: q.EstimatedGas, maxGas: q.MaxGas,
    walletAddress: q.WalletAddress, walletFeePreview: true, emulatedMessageHash: q.EmulatedMessageHash, tonRecipient: q.Recipient,
    tonAuthorizationHash: q.TON.AuthorizationHash, tonUserSeqno: q.TON.UserSeqno, tonValidUntil: Math.floor(now / 1000) + 300,
    tonSimulatedGas: q.TON.SimulatedGas, tonSenderPaidGas: q.TON.SenderPaidGas, tonJettonWalletCode: q.TON.JettonWalletCode,
    relayAttached: q.RelayAttached, relaySurplus: q.RelaySurplus, jettonExcess: q.JettonExcess, accountFunding: q.AccountFunding, accountFundingUsd: q.AccountFundingUSD };
  vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode) => {
    if (path.endsWith('/preview')) return decode({ data: terms });
    if (path.endsWith('/confirm')) return decode({ data: { quote: terms, confirmedAt: now + 1 } });
    return decode({ data: { quoteIdx: q.QuoteIdx, state: 'credited', dispatchBodyHash: 'd'.repeat(64), receiveTxHash: 'e'.repeat(64),
      actualFeeNano: '3546798', jettonSenderFeeNano: '12', recoverableFeeNano: '3546786', jettonExcessNano: '47786383', jettonReserveNano: '0',
      relaySurplusNano: '2526650', creditedUsd: '0.01' } });
  });
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<MemoryRouter><I18nextProvider i18n={i18n}><DepositEstimate asset={asset} native={native} target="0.01" walletAddress={q.WalletAddress}
    local={{ userIdx: 'self', walletId: 'w5' }}/></I18nextProvider></MemoryRouter>);
  fireEvent.click(screen.getByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByText(q.TON.SenderPaidGas + ' TON')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Confirm quote' }));
  expect(await screen.findByRole('button', { name: 'Authorize test USDT deposit' })).toBeEnabled();
  fireEvent.click(screen.getByRole('button', { name: 'Check TON deposit' }));
  expect(await screen.findByText('3546786 nanoTON')).toBeInTheDocument();
  for (const amount of ['12','3546798','47786383','2526650','0']) expect(screen.getByText(amount + ' nanoTON')).toBeInTheDocument();
  expect(screen.getByRole('link')).toHaveAttribute('href', '/records?kind=usd_balance');
});

it('keeps the recipient fixed between the SOL estimate and wallet quote and rejects a self quote', async () => {
  const beneficiaryIdx = '01234567890123456', address = '9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf';
  const sol = { id: 'solana-devnet:sol', symbol: 'SOL', contract: '', origin: 'native' as const, decimals: 9, depositPreview: true, beneficiaryDeposit: true };
  vi.spyOn(catalog, 'getCatalog').mockResolvedValue([{ id: 'solana-devnet', family: 'solana', testnet: true, enabled: true, assets: [sol] }]);
  vi.spyOn(walletSource, 'useViewingWallets').mockReturnValue({ wallets: [{ id: 'w', name: 'Pay wallet', source: 'local', accounts: [{ network: 'solana', address }] }],
    loading: false, error: undefined, connectionError: false, refresh: vi.fn() });
  const preview = vi.spyOn(client, 'userRequest').mockImplementation(async (_path, decode, options) => {
    const { walletAddress, ...input } = options?.body as { walletAddress?: string; targetUsd: string; assetId: string; beneficiaryIdx: string };
    expect(input).toEqual({ targetUsd: '10', assetId: sol.id, beneficiaryIdx });
    const terms = { ...quote(), assetId: sol.id, gasAssetId: sol.id, payQuantity: '0.101', walletFeePreview: true };
    return decode({ data: walletAddress ? { ...terms, walletAddress, messageSha256: 'a'.repeat(64) } : { ...terms, beneficiaryIdx } });
  });
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<I18nextProvider i18n={i18n}><BeneficiaryDeposit userIdx="payer" beneficiaryIdx={beneficiaryIdx} target="10" selected disabled={false} onSelect={vi.fn()}/></I18nextProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Get estimate' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Choose payment wallet' }));
  fireEvent.click(await screen.findByRole('button', { name: /Pay wallet/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Get network fee for this wallet' }));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  expect(preview).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole('button', { name: 'Confirm quote' })).toBeNull();
});
