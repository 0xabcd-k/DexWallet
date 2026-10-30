import { test, expect } from '@playwright/test';
import { envelope, mockAccount, seedSession } from './auth-fixture';
const base = 'http://127.0.0.1:5173';
const deposit = { idx: 'entry-2', voucherIdx: 'voucher-2', businessIdx: '01234567890123456', businessType: 'deposit', action: 'post', kind: 'usd_balance', unit: 'USD', direction: 'in', amount: '9007199254740993.01', balanceAfter: '9007199254740993.01', createdAt: '2026-10-30T08:00:00Z' };
const debit = { ...deposit, idx: 'entry-1', voucherIdx: 'voucher-1', businessType: 'transfer', direction: 'out', amount: '5.00', balanceAfter: '9007199254740988.01' };
const points = { ...deposit, idx: 'points-1', businessType: 'reward', kind: 'task_points', unit: 'POINT', amount: '9007199254740993', balanceAfter: '9007199254740993' };
test.use({ viewport: { width: 390, height: 844 } });

test('records require sign-in, preserve the requested account and show an honest empty state', async ({ page }) => {
  await mockAccount(page);
  await page.goto(`${base}/#/records?kind=reward_usd`);
  await expect(page.getByText('Sign in to view your account records.')).toBeVisible();
  await page.getByRole('link', { name: 'Sign in', exact: true }).click();
  await page.getByLabel('Email', { exact: true }).fill('Alice@example.com');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/records?kind=reward_usd`);
  await expect(page.getByRole('combobox', { name: 'Account', exact: true })).toHaveValue('reward_usd');
  await expect(page.getByText('No account changes yet.')).toBeVisible();
});

test('records paginate, filter, refresh and translate while keeping exact amounts and direction', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  await page.route('**/api/v1/user/account-entries?*', route => {
    const query = new URL(route.request().url()).searchParams;
    const kind = query.get('kind');
    return route.fulfill({ json: envelope(kind === 'task_points' ? { entries: [points], nextCursor: '' } : kind === 'reward_usd' ? { entries: [], nextCursor: '' } : query.has('before') ? { entries: [debit], nextCursor: '' } : { entries: [deposit], nextCursor: 'entry-2' }) });
  });
  await page.goto(`${base}/#/wallet`);
  await page.getByRole('link', { name: 'View records', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/records?kind=usd_balance`);
  await expect(page.getByRole('article')).toContainText('+9,007,199,254,740,993.01 USD');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('article')).toHaveCount(2);
  await expect(page.getByRole('article').last()).toContainText('Out −5.00 USD');
  await page.getByRole('button', { name: 'Refresh records' }).click();
  await expect(page.getByRole('article')).toHaveCount(1);
  await page.getByRole('combobox').selectOption('task_points');
  await expect(page.getByRole('article')).toContainText('+9,007,199,254,740,993 points');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await page.getByRole('combobox').selectOption('zh-CN');
  await page.getByRole('link', { name: '返回', exact: true }).click();
  await expect(page.getByRole('article')).toContainText('增加 +9,007,199,254,740,993 积分');
  await page.screenshot({ path: `test-results/ledger-records-${test.info().project.name}.png`, fullPage: true });
  await page.getByRole('combobox').selectOption('reward_usd');
  await expect(page.getByText('暂无账户变动。')).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(0);
});

test('failed pages preserve displayed records and retry without duplicates or false empty states', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  let failure = true; let malformed = true;
  await page.route('**/api/v1/user/account-entries?*', route => {
    if (!new URL(route.request().url()).searchParams.has('before')) return route.fulfill({ json: envelope({ entries: [deposit], nextCursor: 'entry-2' }) });
    if (failure) return route.fulfill({ status: 503, json: { code: 'SERVICE_UNAVAILABLE', message: 'unavailable', traceId: 'test' } });
    return route.fulfill({ json: envelope({ entries: [malformed ? { ...debit, amount: 5 } : debit], nextCursor: '' }) });
  });
  await page.goto(`${base}/#/records`);
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(page.getByText('No account changes yet.')).toHaveCount(0);
  failure = false;
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(1);
  malformed = false;
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('article')).toHaveCount(2);
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('filter changes and sign-out discard late statement responses', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  let release: () => void = () => {}; const blocked = new Promise<void>(resolve => { release = resolve; });
  let started: () => void = () => {}; const reading = new Promise<void>(resolve => { started = resolve; });
  let releaseSignOut: () => void = () => {};
  await page.route('**/api/v1/user/account-entries?*', async route => {
    if (new URL(route.request().url()).searchParams.get('kind') === 'task_points') return route.fulfill({ json: envelope({ entries: [points], nextCursor: '' }) });
    started(); await blocked;
    await route.fulfill({ json: envelope({ entries: [deposit], nextCursor: '' }) }).catch(() => {});
  });
  try {
    await page.goto(`${base}/#/records`); await reading;
    await page.getByRole('combobox').selectOption('task_points');
    await expect(page.getByRole('article')).toContainText('points');
    release();
    await expect(page.getByRole('article')).not.toContainText('USD');
    const pendingSignOut = new Promise<void>(resolve => { releaseSignOut = resolve; });
    let startedSignOut: () => void = () => {};
    const readingSignOut = new Promise<void>(resolve => { startedSignOut = resolve; });
    await page.route('**/api/v1/user/account-entries?*', async route => {
      startedSignOut(); await pendingSignOut;
      await route.fulfill({ json: envelope({ entries: [deposit], nextCursor: '' }) }).catch(() => {});
    });
    await page.getByRole('combobox').selectOption('usd_balance');
    await readingSignOut;
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    releaseSignOut();
    await page.goto(`${base}/#/records`);
    await expect(page.getByText('Sign in to view your account records.')).toBeVisible();
    await expect(page.getByRole('article')).toHaveCount(0);
  } finally { release(); releaseSignOut(); }
});
