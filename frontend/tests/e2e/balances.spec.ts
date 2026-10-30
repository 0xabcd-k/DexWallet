import { test, expect } from '@playwright/test';
import { envelope, mockAccount, seedSession } from './auth-fixture';

const base = 'http://127.0.0.1:5173';
const accounts = [
  { kind: 'usd_balance', unit: 'USD', balance: '9007199254740993.01', heldBalance: '2.09', totalBalance: '9007199254740995.10' },
  { kind: 'reward_usd', unit: 'USD', balance: '2.35', heldBalance: '0.00', totalBalance: '2.35' },
  { kind: 'task_points', unit: 'POINT', balance: '9007199254740993', heldBalance: '0', totalBalance: '9007199254740993' },
];
test.use({ viewport: { width: 390, height: 844 } });

test('balances require sign-in and return to the requested page after email login', async ({ page }) => {
  await mockAccount(page);
  let reads = 0;
  page.on('request', request => { if (request.url().endsWith('/v1/user/accounts')) reads++; });
  await page.goto(`${base}/#/rewards`);
  await expect(page.getByText('Sign in to view your balances.')).toBeVisible();
  expect(reads).toBe(0);
  await page.getByRole('link', { name: 'Sign in', exact: true }).click();
  await page.getByLabel('Email', { exact: true }).fill('Alice@example.com');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/rewards`);
  await expect(page.getByRole('region', { name: 'Withdrawable rewards' })).toContainText('0.00 USD');
  await expect(page.getByRole('region', { name: 'Task points' })).toContainText('0 points');
});

test('wallet and rewards show exact independent balances and translate without resetting them', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  await page.route('**/api/v1/user/accounts', route => route.fulfill({ json: envelope({ accounts }) }));
  await page.goto(`${base}/#/wallet`);
  const wallet = page.getByRole('region', { name: 'USD balance' });
  await expect(wallet).toContainText('9,007,199,254,740,993.01 USD');
  await expect(wallet).toContainText('2.09 USD');
  await expect(wallet).toContainText('9,007,199,254,740,995.10 USD');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: `test-results/balances-wallet-${test.info().project.name}.png`, fullPage: true });
  await page.getByRole('link', { name: 'Rewards', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Withdrawable rewards' })).toContainText('2.35 USD');
  await expect(page.getByRole('region', { name: 'Task points' })).toContainText('9,007,199,254,740,993 points');
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await page.getByRole('combobox').selectOption('zh-CN');
  await page.getByRole('link', { name: '返回', exact: true }).click();
  await expect(page.getByRole('region', { name: '任务积分' })).toContainText('9,007,199,254,740,993 积分');
  await page.screenshot({ path: `test-results/balances-rewards-${test.info().project.name}.png`, fullPage: true });
});

test('failed or malformed reads show errors and can retry without inventing a zero balance', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  let mode: 'unavailable' | 'malformed' | 'ok' = 'unavailable';
  await page.route('**/api/v1/user/accounts', route => {
    if (mode === 'unavailable') return route.fulfill({ status: 503, json: { code: 'SERVICE_UNAVAILABLE', message: 'unavailable', traceId: 'test' } });
    return route.fulfill({ json: envelope({ accounts: mode === 'malformed' ? [] : accounts }) });
  });
  await page.goto(`${base}/#/wallet`);
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('region', { name: 'USD balance' })).toHaveCount(0);
  mode = 'malformed';
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('region', { name: 'USD balance' })).toHaveCount(0);
  mode = 'ok';
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByRole('region', { name: 'USD balance' })).toContainText('9,007,199,254,740,993.01 USD');
});

test('a late balance response cannot restore the display after sign-out', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  let release: () => void = () => {};
  const pending = new Promise<void>(resolve => { release = resolve; });
  let started: () => void = () => {};
  const reading = new Promise<void>(resolve => { started = resolve; });
  await page.route('**/api/v1/user/accounts', async route => {
    started(); await pending;
    await route.fulfill({ json: envelope({ accounts }) }).catch(() => {});
  });
  try {
    await page.goto(`${base}/#/wallet`);
    await reading;
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    release();
    await page.goto(`${base}/#/wallet`);
    await expect(page.getByText('Sign in to view your balances.')).toBeVisible();
    await expect(page.getByRole('region', { name: 'USD balance' })).toHaveCount(0);
  } finally { release(); }
});
