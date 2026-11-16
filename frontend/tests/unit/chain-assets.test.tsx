import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import type { ChainNetwork, ChainAssetBalance } from '../../packages/api-contract/src/platform';
import { appResources } from '../../apps/user/src/locales';
import { AssetBrowser } from '../../apps/user/src/view/chain-assets';
import { decodeCatalog, decodeQuantities } from '../../apps/user/src/app/native/chain-assets/api';
import { addressFor, favoritesStore, type ViewingWallet } from '../../apps/user/src/app/native/chain-assets/wallets';
import { boundaries } from './local-wallet-fixture';

const base: ChainNetwork = { id: 'base-mainnet', family: 'base', testnet: false, enabled: true, assets: [
  { id: 'base-mainnet:eth', symbol: 'ETH', contract: '', decimals: 18, origin: 'native' },
  { id: 'base-mainnet:usdc', symbol: 'USDC', contract: '0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913', decimals: 6, origin: 'issuer' },
] };
const testnet: ChainNetwork = { ...base, id: 'base-sepolia', testnet: true, assets: base.assets.map(a => ({ ...a, id: a.id.replace('mainnet','sepolia') })) };
const wallets: ViewingWallet[] = [
  { id: 'a', name: 'Primary', source: 'local', accounts: [{ network: 'base', address: '0x0000000000000000000000000000000000000001' }] },
  { id: 'b', name: 'Second', source: 'local', accounts: [{ network: 'base', address: '0x0000000000000000000000000000000000000002' }] },
];
function balances(network: ChainNetwork, quantity = '0'): ChainAssetBalance[] { return network.assets.map(a => ({ assetId: a.id, quantity, status: 'ok', errorCode: 'NONE', queriedAt: 1794800000000 })); }
async function setup(quantities: (n: ChainNetwork, address: string, signal: AbortSignal) => Promise<ChainAssetBalance[]> = vi.fn(async (n: ChainNetwork) => balances(n))) {
  const f = boundaries(), favorites = favoritesStore('user-a', f.catalog);
  const i18n = await createAppI18n('user', appResources, 'en');
  const catalog = vi.fn(async () => [base, testnet]);
  render(<I18nextProvider i18n={i18n}><AssetBrowser wallets={wallets} favorites={favorites} quantities={quantities} catalog={catalog}/></I18nextProvider>);
  await screen.findByRole('combobox', { name: 'Network' });
  return { ...f, quantities, favorites };
}
describe('read-only asset boundary', () => {
  it('keeps exact decimal strings and rejects wrong address, precision and zero-on-error', () => {
    expect(decodeCatalog({ data: { networks: [base, testnet] } })).toEqual([base, testnet]);
    const address = wallets[0]!.accounts[0]!.address, decode = decodeQuantities(base, address);
    const payload = { data: { network: base.id, address, items: balances(base) } };
    payload.data.items[0]!.quantity = '9007199254740993.000000000000000001';
    expect(decode(payload)[0]!.quantity).toBe(payload.data.items[0]!.quantity);
    expect(() => decode({ data: { ...payload.data, address: wallets[1]!.accounts[0]!.address } })).toThrow();
    payload.data.items[1]!.quantity = '1.0000001'; expect(() => decode(payload)).toThrow();
    payload.data.items[1] = { ...payload.data.items[1]!, quantity: '0', status: 'unavailable', errorCode: 'UPSTREAM_INVALID' }; expect(() => decode(payload)).toThrow();
    payload.data.items[1]!.quantity = ''; expect(decode(payload)[1]!.quantity).toBe('');
    payload.data.items[1]!.assetId = 'base-mainnet:other-token'; expect(() => decode(payload)).toThrow();
    expect(() => decodeCatalog({ data: { networks: [{ ...base, assets: [{ ...base.assets[1], origin: 'test_fixture' }] }] } })).toThrow();
  });
  it('isolates favorites between product users and TON networks', async () => {
    const f = boundaries(), a = favoritesStore('a', f.catalog), b = favoritesStore('b', f.catalog);
    const ids = ['base-mainnet:usdc', 'ton-testnet:usdt:kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy'];
    await a.write(ids); expect(await b.read()).toEqual([]); expect(await a.read()).toEqual(ids);
    const wallet: ViewingWallet = { id: 'external', name: 'TON', source: 'external', accounts: [{ network: 'ton-testnet', address: 'test-address' }] };
    expect(addressFor(wallet, { ...base, id: 'ton-mainnet', family: 'ton' })).toBeUndefined();
    expect(addressFor(wallet, { ...base, id: 'ton-testnet', family: 'ton', testnet: true })).toBe('test-address');
  });
});
describe('asset browsing', () => {
  it('shows zero and unavailable distinctly, searches full contracts and persists favorites', async () => {
    const f = await setup(vi.fn(async (n: ChainNetwork): Promise<ChainAssetBalance[]> => [balances(n)[0]!, { ...balances(n)[1]!, quantity: '', status: 'unavailable', errorCode: 'UPSTREAM_UNAVAILABLE' }]));
    await screen.findByText('Temporarily unavailable');
    expect(within(screen.getByTestId('asset-base-mainnet:eth')).getByText('0')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Search asset, network or token address'), { target: { value: base.assets[1]!.contract } });
    expect(screen.queryByTestId('asset-base-mainnet:eth')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Favorite USDC' }));
    await screen.findByRole('button', { name: 'Unfavorite USDC' }); expect(await f.favorites.read()).toEqual(['base-mainnet:usdc']);
    fireEvent.change(screen.getByLabelText('Search asset, network or token address'), { target: { value: '' } });
    expect(screen.getAllByRole('listitem')[0]).toHaveAttribute('data-testid', 'asset-base-mainnet:usdc');
  });
  it('clears previous holdings immediately and discards late results when switching wallet and network', async () => {
    const pending: { network: ChainNetwork; address: string; signal: AbortSignal; resolve: (items: ChainAssetBalance[]) => void }[] = [];
    const quantities = vi.fn((network: ChainNetwork, address: string, signal: AbortSignal) => new Promise<ChainAssetBalance[]>(resolve => pending.push({ network, address, signal, resolve })));
    await setup(quantities);
    await waitFor(() => expect(pending).toHaveLength(1));
    fireEvent.change(screen.getByLabelText('Viewing wallet'), { target: { value: 'b' } });
    await waitFor(() => expect(pending).toHaveLength(2)); expect(pending[0]!.signal.aborted).toBe(true);
    await act(async () => pending[0]!.resolve(balances(base, '99'))); expect(screen.queryByText('99')).toBeNull();
    await act(async () => pending[1]!.resolve(balances(base, '12'))); expect(screen.getAllByText('12')).toHaveLength(2);
    fireEvent.change(screen.getByLabelText('Network'), { target: { value: 'base-sepolia' } });
    expect(screen.queryByText('12')).toBeNull(); expect(screen.getByText('Test network · test assets have no monetary value.')).toBeInTheDocument();
    await waitFor(() => expect(pending).toHaveLength(3));
    await act(async () => pending[2]!.resolve(balances(testnet, '3'))); expect(screen.getAllByText('3')).toHaveLength(2);
  });
  it('keeps a failed query unavailable and supports an explicit refresh', async () => {
    const quantities = vi.fn(async (n: ChainNetwork) => balances(n)).mockRejectedValueOnce(new Error('offline'));
    await setup(quantities); await screen.findByRole('alert'); expect(screen.queryByText('0')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Refresh balances' }));
    await waitFor(() => expect(screen.getAllByText('0')).toHaveLength(2));
  });
});
