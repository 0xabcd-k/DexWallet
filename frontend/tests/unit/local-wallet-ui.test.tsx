import { StrictMode } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import { appResources } from '../../apps/user/src/locales';
import { WalletManager } from '../../apps/user/src/view/local-wallets';
import type { NativeWalletVault, PrivacyState } from '../../apps/user/src/app/native/local-wallet/bridge';
import { boundaries, mnemonic } from './local-wallet-fixture';

vi.mock('@capacitor/app', () => ({ App: { getState: async () => ({ isActive: true }) } }));
async function setup() {
  const f = boundaries();
  let state: PrivacyState = { token: '', active: true, revision: 0 };
  const listeners = new Set<(state: PrivacyState) => void>();
  const vault: NativeWalletVault = {
    ...f.vault,
    async addListener(_event, listener) { listeners.add(listener); return { async remove() { listeners.delete(listener); } }; },
    async protect({ token }) { state = { token, active: true, revision: state.revision + 1 }; return state; },
    acknowledge: vi.fn(async () => {}),
    async unprotect() {},
  };
  const i18n = await createAppI18n('user', appResources, 'en');
  const view = render(<StrictMode><I18nextProvider i18n={i18n}><WalletManager userIdx="ui-user" vault={vault} catalog={f.catalog}/></I18nextProvider></StrictMode>);
  await screen.findByRole('button', { name: 'Create wallet' });
  function privacy(active: boolean) { state = { ...state, active, revision: state.revision + 1 }; for (const listener of listeners) listener(state); }
  return { ...f, vault, privacy, view, listeners };
}
const click = (name: string) => fireEvent.click(screen.getByRole('button', { name }));
const fill = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
async function create() {
  click('Create wallet'); fill('Wallet name', 'Primary'); click('Create and save on this device');
  await screen.findByRole('heading', { name: 'Primary' });
}
function addresses() { return [...document.querySelectorAll('[data-testid^="address-"]')].map(e => e.textContent); }
describe('built-in wallet management', () => {
  it('creates, checks a backup, removes with acknowledgement and imports the same addresses', async () => {
    const f = await setup(); await create();
    const original = addresses();
    expect(original).toHaveLength(5);
    expect(screen.getByText('Backup not checked')).toBeInTheDocument();
    click('Back up recovery phrase');
    const list = await screen.findByRole('list');
    const words = within(list).getAllByRole('listitem').map(e => e.textContent!);
    expect(words).toHaveLength(24);
    click('I wrote them down — check backup');
    const inputs = screen.getAllByRole('textbox');
    for (const input of inputs) fireEvent.change(input, { target: { value: 'incorrect' } });
    click('Confirm backup');
    await screen.findByText('The words do not match. Check your backup and try again.');
    expect([...f.metadata.values()][0]).toContain('"backedUp":false');
    for (const input of inputs) {
      const number = Number(input.closest('label')!.textContent!.match(/\d+/)![0]);
      fireEvent.change(input, { target: { value: words[number - 1] } });
    }
    click('Confirm backup'); await screen.findByText('Backup check completed. Keep the recovery phrase safe.');
    expect(document.querySelector('[data-private]')).toBeNull();
    click('Remove from this device'); expect(screen.getByRole('button', { name: 'Confirm removal' })).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox')); click('Confirm removal');
    await screen.findByText('No built-in wallets on this device for this account.');
    expect(f.secrets.size).toBe(0);
    click('Import wallet'); fill('Wallet name', 'Restored'); fill('Recovery phrase', words.join(' '));
    click('Check recovery addresses'); await screen.findByRole('button', { name: 'Confirm addresses and save' });
    expect(addresses()).toEqual(original); expect(screen.queryByLabelText('Recovery phrase')).toBeNull();
    click('Confirm addresses and save'); await screen.findByRole('heading', { name: 'Restored' });
    expect(addresses()).toEqual(original); expect(f.secrets.size).toBe(1);
    expect([...f.metadata.values()].join()).not.toContain(words.join(' '));
  });
  it('leaves no wallet behind when system confirmation is cancelled', async () => {
    const f = await setup(); f.control.rejectPut = 'VAULT_CANCELLED';
    click('Create wallet'); fill('Wallet name', 'Cancelled'); click('Create and save on this device');
    await screen.findByText('Device confirmation was cancelled.');
    expect(f.secrets.size).toBe(0); expect(JSON.parse([...f.metadata.values()][0]!).items).toEqual([]);
    click('Cancel'); expect(screen.queryByRole('heading', { name: 'Cancelled' })).toBeNull();
  });
  it('shows a missing local key after an uncertain save without offering an impossible unlock', async () => {
    const f = await setup(); f.control.rejectPut = 'VAULT_STORAGE_FAILED';
    click('Create wallet'); fill('Wallet name', 'Interrupted'); click('Create and save on this device');
    await screen.findByRole('alert'); click('Cancel');
    expect(screen.getByText('Local key record is missing. Restore from your backup or remove this entry.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Unlock and check record' })).toBeNull();
    expect(f.control.puts).toBe(1);
  });
  it('clears revealed words and import drafts on background, before acknowledging the foreground cover', async () => {
    const f = await setup(); await create(); click('Back up recovery phrase'); await screen.findByRole('list');
    f.vault.acknowledge = vi.fn(async () => { expect(document.querySelector('[data-private]')).toBeNull(); });
    act(() => f.privacy(false)); expect(document.querySelector('[data-private]')).toBeNull();
    act(() => f.privacy(true)); await waitFor(() => expect(f.vault.acknowledge).toHaveBeenCalled());
    click('Import wallet'); fill('Wallet name', 'Draft'); fill('Recovery phrase', mnemonic);
    act(() => f.privacy(false)); act(() => f.privacy(true));
    expect(screen.getByLabelText('Recovery phrase')).toHaveValue('');
    f.view.unmount(); expect(f.listeners.size).toBe(0);
  });
  it('does not reveal a late unlock result after the page loses foreground', async () => {
    const f = await setup(); await create();
    let finish!: () => void;
    const original = f.vault.get;
    f.vault.get = async value => { const result = await original(value); await new Promise<void>(resolve => { finish = resolve; }); return result; };
    click('Back up recovery phrase'); await waitFor(() => expect(finish).toBeTypeOf('function'));
    act(() => f.privacy(false)); await act(async () => finish());
    act(() => f.privacy(true));
    await screen.findByText('The wallet view was closed or hidden. Unlock again to view recovery words.');
    expect(document.querySelector('[data-private]')).toBeNull();
  });
});
