import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChainNetwork, DepositQuoteData } from '../../packages/api-contract/src/platform';
import * as client from '../../apps/user/src/api/account';
import * as catalog from '../../apps/user/src/app/native/chain-assets/api';
import * as walletSource from '../../apps/user/src/app/native/chain-assets/use-wallets';
import { previewDeposit } from '../../apps/user/src/app/native/chain-transfer/deposit';
import DepositButton, { DepositEstimate } from '../../apps/user/src/view/chain-transfer/deposit';
import { appResources } from '../../apps/user/src/locales';

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
  fireEvent.click(screen.getByRole('button'));
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
  const asset = { ...network.assets[1]!, id, symbol, contract };
  const wallet = '9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf';
  const result = { ...quote(), assetId: asset.id, gasAssetId: native.id, usdPerCoin: rate, targetUsd: '0.01', netUsd: '0.01',
    estimatedGas: '0.00001', gasUsd: '0.001', payQuantity: pay, walletAddress: wallet, walletFeePreview: true, messageSha256: 'a'.repeat(64) };
  vi.spyOn(client, 'userRequest').mockImplementationOnce(async (_path, decode) =>
    decode({ data: { ...result, accountFunding: funding, accountFundingUsd: usd } })).mockImplementationOnce(async (_path, decode) =>
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
