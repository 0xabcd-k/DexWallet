import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChainNetwork, DepositQuoteData } from '../../packages/api-contract/src/platform';
import * as client from '../../apps/user/src/api/account';
import * as catalog from '../../apps/user/src/app/native/chain-assets/api';
import DepositButton from '../../apps/user/src/view/chain-transfer/deposit';
import { appResources } from '../../apps/user/src/locales';

const network: ChainNetwork = { id: 'base-sepolia', family: 'base', enabled: false, testnet: true, assets: [
  { id: 'base-sepolia:eth', symbol: 'ETH', contract: '', decimals: 18, origin: 'native', depositPreview: true },
  { id: 'base-sepolia:usdc', symbol: 'USDC', contract: 'test-usdc', decimals: 6, origin: 'issuer', depositPreview: true },
] };
const quote = (): DepositQuoteData => ({ assetId: 'base-sepolia:usdc', targetUsd: '10.00', usdPerCoin: '0.98',
  payQuantity: '10.408164', gasAssetId: 'base-sepolia:eth', estimatedGas: '0.0001', gasUsd: '0.2', netUsd: '10.00',
  rateVersion: 'test-1', asOf: Date.now()-10000, quotedAt: Date.now(), expiresAt: Date.now()+30000, canSubmit: false });
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
  expect(await screen.findByText('No current test rates are available.')).toBeInTheDocument();
  expect(screen.queryByRole('combobox')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
});
