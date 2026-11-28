import { MemoryRouter } from 'react-router';
import { fireEvent, render, screen } from '@testing-library/react';
import { createAppI18n, I18nextProvider } from '@dexwallet/i18n';
import { afterAll, beforeAll, expect, it, vi } from 'vitest';
import { appResources } from '../../apps/user/src/locales';
import * as client from '../../apps/user/src/api/account';
import * as catalog from '../../apps/user/src/app/native/chain-assets/api';

const recipient = '09876543210987654', receiveCode = 'https://app.test/pay?code=personal-fixture';
vi.mock('../../apps/user/src/auth/context', () => ({
  useAuth: () => ({ session: { generation: 1, credential: { issuer: 'test', idx: '01234567890123456' } }, launch: { mode: 'app' } }),
  Failure: () => <p role="alert">Request failed</p>,
}));
vi.mock('../../apps/user/src/auth/session', () => ({ session: { capture: () => ({}), matches: () => true } }));
vi.mock('../../apps/user/src/view/transfers/scanner', () => ({
  Scanner: ({ onScan }: { onScan: (s: string) => void }) => <button onClick={() => onScan(receiveCode)}>Scan fixture</button>,
}));
let Pay: typeof import('../../apps/user/src/view/transfers/pay').default;
beforeAll(async () => {
  vi.stubEnv('NATIVE_APP', 'true');
  Pay = (await import('../../apps/user/src/view/transfers/pay')).default;
});
afterAll(() => vi.unstubAllEnvs());

it.each(['idx', 'scan'])('opens native SOL funding for the resolved %s even with zero USD balance', async mode => {
  const target = mode === 'idx' ? { recipientIdx: recipient } : { receiveCode };
  vi.spyOn(catalog, 'getCatalog').mockResolvedValue([{ id: 'solana-devnet', family: 'solana', enabled: true, testnet: true,
    assets: [{ id: 'solana-devnet:sol', symbol: 'SOL', contract: '', decimals: 9, origin: 'native', depositPreview: true, beneficiaryDeposit: true }] }]);
  const requests = vi.spyOn(client, 'userRequest').mockImplementation(async (path, decode, options) => {
    if (path.endsWith('/resolve')) {
      expect(options?.body).toEqual(target);
      return decode({ data: { idx: recipient, nickname: 'Receiver B', avatarUrl: '' } });
    }
    if (path.endsWith('/accounts')) return decode({ data: { accounts: [
      ['usd_balance', 'USD', '0.00'], ['reward_usd', 'USD', '0.00'], ['task_points', 'POINT', '0'],
    ].map(([kind, unit, balance]) => ({ kind, unit, balance, heldBalance: balance, totalBalance: balance })) } });
    if (path.endsWith('/transfer-policy')) return decode({ data: { resetAt: new Date(Date.now() + 3600000).toISOString(),
      remainingToday: '0', plan: { minAmount: '0.01', maxAmount: '100', dailyOutgoingLimit: '100' } } });
    if (path.endsWith('/deposit-quotes/preview')) {
      const now = Date.now();
      return decode({ data: { assetId: 'solana-devnet:sol', gasAssetId: 'solana-devnet:sol', beneficiaryIdx: recipient,
        targetUsd: '10', netUsd: '10.00', usdPerCoin: '100', payQuantity: '0.10001', estimatedGas: '0.00001', gasUsd: '0.001',
        asOf: now - 1000, quotedAt: now, expiresAt: now + 30000, rateVersion: 'test', walletFeePreview: true, canSubmit: false } });
    }
    throw new Error('Unexpected request ' + path);
  });
  const i18n = await createAppI18n('user', appResources, 'en');
  render(<MemoryRouter initialEntries={[mode === 'idx' ? '/pay?manual=1' : '/pay']}><I18nextProvider i18n={i18n}><Pay/></I18nextProvider></MemoryRouter>);
  if (mode === 'idx') fireEvent.change(screen.getByLabelText('Recipient ID'), { target: { value: recipient } });
  else fireEvent.click(screen.getByText('Scan fixture'));
  fireEvent.change(screen.getByLabelText('Transfer amount (USD)'), { target: { value: '10' } });
  fireEvent.click(screen.getByRole('button', { name: 'Review payment' }));
  expect(await screen.findByText('Receiver B')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Confirm payment' })).toBeDisabled();
  fireEvent.click(await screen.findByRole('button', { name: 'Pay from a Solana wallet' }));
  expect(screen.getByText(recipient)).toBeInTheDocument();
  fireEvent.click(await screen.findByRole('button', { name: 'Get estimate' }));
  expect(await screen.findByText('0.10001 SOL')).toBeInTheDocument();
  expect(requests.mock.calls.at(-1)?.[2]?.body).toMatchObject({ beneficiaryIdx: recipient, assetId: 'solana-devnet:sol', targetUsd: '10' });
  expect(requests.mock.calls.some(([path]) => path.endsWith('/transfers'))).toBe(false);
});
