import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import type { ReactNode } from 'react';
import type { ChainNetwork, ChainTargetData, ChainTransferPreviewData } from '../../packages/api-contract/src/platform';
import { appResources } from '../../apps/user/src/locales';
import { receiveFields, isChainInput, chainPreparationRoute } from '../../apps/user/src/app/chain-intent';
import { installNativeLifecycle, nativeDestination } from '../../apps/user/src/app/native/lifecycle';
import { decodePreview } from '../../apps/user/src/app/native/chain-transfer/api';
import { ChainReceiveForm } from '../../apps/user/src/view/chain-transfer/receive';
import { PreparationForm } from '../../apps/user/src/view/chain-transfer';
import type { ViewingWallet } from '../../apps/user/src/app/native/chain-assets/wallets';

const network: ChainNetwork = { id: 'base-sepolia', family: 'base', testnet: true, enabled: false, assets: [
  { id: 'base-sepolia:eth', symbol: 'ETH', contract: '', decimals: 18, origin: 'native' },
  { id: 'base-sepolia:usdc', symbol: 'USDC', contract: '0x036CbD53842c5426634e7929541eC2318f3dCF7e', decimals: 6, origin: 'issuer' },
] };
const networks = [network], address = '0x0000000000000000000000000000000000000001';
const wallets: ViewingWallet[] = [
  { id: 'local', name: 'Primary', source: 'local', accounts: [{ network: 'base', address }] },
  { id: 'external', name: 'TON external', source: 'external', accounts: [{ network: 'ton-testnet', address: 'other-network' }] },
];
function target(assetId = network.assets[0]!.id): ChainTargetData {
  return { network: network.id, assetId, address, receiveCode: `com.dexwallet.app://chain-transfer?address=${address}&asset=${encodeURIComponent(assetId)}&network=base-sepolia&v=1` };
}
const result = (amount = '1.000000000000000001'): ChainTransferPreviewData => ({ target: target(), amount, canSubmit: false, blockReason: 'CHANNEL_NOT_OPEN' });
async function setup(node: ReactNode) {
  const i18n = await createAppI18n('user', appResources, 'en');
  return render(<I18nextProvider i18n={i18n}><MemoryRouter>{node}</MemoryRouter></I18nextProvider>);
}
describe('chain target trust boundary', () => {
  it('waits for cold-start lazy navigation before the initial screen can redirect', async () => {
    const nativeApp = { addListener: vi.fn(async () => ({ remove: async () => {} })), getLaunchUrl: async () => ({ url: target().receiveCode }) } as unknown as Parameters<typeof installNativeLifecycle>[1];
    let finish!: () => void;
    const navigate = vi.fn(() => new Promise<void>(r => { finish = r; }));
    const router = { navigate } as unknown as Parameters<typeof installNativeLifecycle>[0];
    let completed = false;
    const installation = installNativeLifecycle(router, nativeApp).then(() => { completed = true; });
    await waitFor(() => expect(navigate).toHaveBeenCalledWith(chainPreparationRoute(target().receiveCode)));
    expect(completed).toBe(false); finish(); await installation; expect(completed).toBe(true);
  });
  it('only routes a versioned destination, rejecting amount, memo, call data and ambiguous links', () => {
    const code = target().receiveCode;
    expect(receiveFields(code)).toEqual({ network: network.id, assetId: network.assets[0]!.id, address });
    expect(nativeDestination(code)).toBe(chainPreparationRoute(code));
    for (const value of [code + '&amount=1', code + '&memo=order', code + '&data=0x00', code + '&v=1', code + '#', code.replace('v=1', 'v=2'), code.replace('chain-transfer?', 'evil@chain-transfer?'), code.replace('base-sepolia%3Aeth', 'base-mainnet%3Aeth'), code.replace('com.dexwallet.app:', 'https:')]) {
      expect(receiveFields(value)).toBeUndefined(); expect(nativeDestination(value)).toBeUndefined();
    }
    expect(isChainInput(address)).toBe(true); expect(receiveFields(address)).toBeUndefined();
    expect(isChainInput('09876543210987654')).toBe(false);
  });
  it('requires the exact target and integer amount and never trusts permission to send', () => {
    const input = { network: network.id, assetId: network.assets[0]!.id, address, amount: '1.000000000000000001' };
    expect(decodePreview({ data: result() }, networks, input)).toEqual(result());
    for (const changed of [{ ...result(), amount: '1' }, { ...result(), canSubmit: true }, { ...result(), blockReason: '' }, { ...result(), target: { ...target(), address: address.replace(/1$/, '2') } }, { ...result(), target: target(network.assets[1]!.id) }]) expect(() => decodePreview({ data: changed }, networks, input)).toThrow();
  });
});
describe('chain receive and review', () => {
  it('clears a previous QR immediately and ignores late results after switching assets', async () => {
    const deferred: { id: string; resolve: (value: ChainTargetData) => void; signal: AbortSignal }[] = [];
    const resolve = vi.fn((input: { assetId?: string }, _networks: ChainNetwork[], signal: AbortSignal) => new Promise<ChainTargetData>(done => deferred.push({ id: input.assetId!, resolve: done, signal })));
    const makeQR = vi.fn(async () => 'data:image/png;base64,fixture');
    await setup(<ChainReceiveForm networks={networks} wallets={wallets} tabs={<p>Receive tabs</p>} resolve={resolve} makeQR={makeQR}/>);
    await waitFor(() => expect(deferred).toHaveLength(1));
    await act(async () => deferred[0]!.resolve(target()));
    expect(screen.getByRole('img')).toHaveAttribute('alt', 'DexWallet on-chain receive code');
    expect(screen.queryByLabelText(/Amount to send/)).toBeNull();
    fireEvent.change(screen.getByLabelText('Asset'), { target: { value: network.assets[1]!.id } });
    expect(screen.queryByRole('img')).toBeNull(); expect(screen.queryByLabelText('DexWallet App receive link')).toBeNull();
    await waitFor(() => expect(deferred).toHaveLength(2)); expect(deferred[0]!.signal.aborted).toBe(true);
    fireEvent.change(screen.getByLabelText('Asset'), { target: { value: network.assets[0]!.id } });
    await waitFor(() => expect(deferred).toHaveLength(3));
    await act(async () => deferred[1]!.resolve(target(network.assets[1]!.id)));
    expect(screen.queryByDisplayValue(target(network.assets[1]!.id).receiveCode)).toBeNull();
    await act(async () => deferred[2]!.resolve(target()));
    expect(screen.getByLabelText('DexWallet App receive link')).toHaveValue(target().receiveCode);
  });
  it('keeps a failed QR unavailable and allows retry without keeping a stale address', async () => {
    const resolve = vi.fn(async () => target()).mockRejectedValueOnce(new Error('offline'));
    await setup(<ChainReceiveForm networks={networks} wallets={wallets} tabs={null} resolve={resolve} makeQR={async () => 'data:image/png;base64,fixture'}/>);
    await screen.findByRole('alert'); expect(screen.queryByRole('img')).toBeNull();
    expect(screen.queryByRole('status')).toBeNull(); fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await screen.findByRole('img');
  });
  it('requires explicit network and asset for a bare address, filters wallets and keeps send disabled', async () => {
    const preview = vi.fn(async () => result());
    await setup(<PreparationForm request={address} networks={networks} wallets={wallets} preview={preview}/>);
    expect(screen.getByLabelText('Network')).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Review payment' })).toBeDisabled();
    fireEvent.change(screen.getByLabelText('Network'), { target: { value: network.id } });
    fireEvent.change(screen.getByLabelText('Asset'), { target: { value: network.assets[0]!.id } });
    fireEvent.change(screen.getByLabelText('Amount to send (ETH)'), { target: { value: result().amount } });
    fireEvent.click(screen.getByRole('button', { name: 'Review payment' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Choose payment wallet' }));
    fireEvent.click(screen.getByRole('button', { name: 'Primary · Built-in' }));
    expect(screen.queryByRole('button', { name: /TON external/ })).toBeNull();
    expect(screen.getByRole('button', { name: 'Confirm payment' })).toBeDisabled();
    expect(screen.queryByText(/Available balance/)).toBeNull();
    expect(screen.getByText('Network fee, total cost and USD valuation are not quoted yet.')).toBeInTheDocument();
    expect(preview).toHaveBeenCalledOnce();
  });
  it('rejects a late preview after the amount changes and fixes scanned destinations', async () => {
    let complete!: (result: ChainTransferPreviewData) => void;
    const resolve = vi.fn(async () => target()), preview = vi.fn(() => new Promise<ChainTransferPreviewData>(r => { complete = r; }));
    await setup(<PreparationForm request={target().receiveCode} networks={networks} wallets={wallets} resolve={resolve} preview={preview}/>);
    await waitFor(() => expect(screen.getByLabelText('Network')).toHaveValue(network.id));
    expect(screen.getByLabelText('Network')).toBeDisabled(); expect(screen.getByLabelText('Recipient address')).toHaveAttribute('readonly');
    fireEvent.change(screen.getByLabelText('Amount to send (ETH)'), { target: { value: '1' } });
    fireEvent.click(screen.getByRole('button', { name: 'Review payment' }));
    fireEvent.change(screen.getByLabelText('Amount to send (ETH)'), { target: { value: '2' } });
    await act(async () => complete(result('1')));
    expect(screen.queryByRole('button', { name: 'Choose payment wallet' })).toBeNull();
  });
});
