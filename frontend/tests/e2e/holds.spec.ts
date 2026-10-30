import { test, expect } from '@playwright/test';
import { envelope, mockAccount, seedSession } from './auth-fixture';
const base = 'http://127.0.0.1:5173';
const held = { idx: 'hold-2', businessType: 'withdrawal', businessIdx: '01234567890123456', amount: '30.00', unit: 'USD', status: 'held', reason: '', holdVoucherIdx: 'voucher', resolvedVoucherIdx: '', createdAt: '2026-10-30T08:00:00Z', resolvedAt: '' };
const released = { ...held, idx: 'hold-1', businessIdx: '01234567890123457', status: 'released', reason: 'rejected', resolvedVoucherIdx: 'resolved', resolvedAt: '2026-10-30T09:00:00Z' };
test.use({ viewport: { width: 390, height: 844 } });
test('wallet reservations show status, pagination, failures and translated resolution', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  let fail = true;
  await page.route('**/api/v1/user/account-holds?*', route => {
    const q = new URL(route.request().url()).searchParams;
    if (q.get('status') === 'consumed') return route.fulfill({ json: envelope({ holds: [], nextCursor: '' }) });
    if (q.get('status') === 'held') return route.fulfill({ json: envelope({ holds: [held], nextCursor: '' }) });
    if (q.has('before') && fail) return route.fulfill({ status: 503, json: { code: 'SERVICE_UNAVAILABLE', message: 'busy', traceId: 'test' } });
    return route.fulfill({ json: envelope(q.has('before') ? { holds: [released], nextCursor: '' } : { holds: [held], nextCursor: 'hold-2' }) });
  });
  await page.goto(`${base}/#/wallet`);
  await page.getByRole('link', { name: 'View reserved funds' }).click();
  await expect(page.getByRole('article')).toContainText('Reserved');
  await expect(page.getByRole('article')).toContainText('30.00 USD');
  await page.getByRole('combobox').selectOption('');
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(1);
  fail = false; await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('article')).toHaveCount(2);
  await expect(page.getByRole('article').last()).toContainText('Rejected');
  await page.getByRole('button', { name: 'Refresh reservations' }).click();
  await expect(page.getByRole('article')).toHaveCount(1);
  await page.getByRole('combobox').selectOption('consumed');
  await expect(page.getByText('No reservations match this filter.')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.goto(`${base}/#/settings`); await page.getByRole('combobox').selectOption('zh-CN');
  await page.goto(`${base}/#/holds?status=held`);
  await expect(page.getByRole('article')).toContainText('占用中');
  await page.screenshot({ path: `test-results/account-holds-${test.info().project.name}.png`, fullPage: true });
});
test('records label reservations separately from the actual charge', async ({ page }) => {
  await mockAccount(page); await seedSession(page);
  const common = { voucherIdx: 'voucher', businessIdx: held.businessIdx, businessType: 'withdrawal', kind: 'usd_balance', unit: 'USD', amount: '30.00', createdAt: held.createdAt };
  await page.route('**/api/v1/user/account-entries?*', route => route.fulfill({ json: envelope({ entries: [
    { ...common, idx: 'consume', action: 'consume', direction: 'out', balanceAfter: '0.00', balancePart: 'held', activity: 'consumed' },
    { ...common, idx: 'hold', action: 'hold', direction: 'out', balanceAfter: '70.00', balancePart: 'available', activity: 'held' },
  ], nextCursor: '' }) }));
  await page.goto(`${base}/#/records`);
  await expect(page.getByRole('article').first()).toContainText('Charged −30.00 USD');
  await expect(page.getByRole('article').first()).toContainText('Held after change');
  await expect(page.getByRole('article').last()).toContainText('Funds reserved 30.00 USD');
  await expect(page.getByRole('article').last()).not.toContainText('−30.00');
});
test('reservations require authentication and discard responses after sign-out', async ({ page }) => {
  await mockAccount(page);
  await page.goto(`${base}/#/holds?status=held`);
  await expect(page).toHaveURL(/login/);
  await page.getByLabel('Email', { exact: true }).fill('Alice@example.com');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/holds?status=held`);
  await expect(page.getByText('No reservations match this filter.')).toBeVisible();
  let release: () => void = () => {}; const blocked = new Promise<void>(r => { release = r; });
  let started: () => void = () => {}; const reading = new Promise<void>(r => { started = r; });
  await page.route('**/api/v1/user/account-holds?*', async route => { started(); await blocked; await route.fulfill({ json: envelope({ holds: [held], nextCursor: '' }) }).catch(() => {}); });
  try {
    await page.getByRole('button', { name: 'Refresh reservations' }).click(); await reading;
    await page.goto(`${base}/#/settings`); await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await expect(page).toHaveURL(`${base}/#/wallet`);
    await expect(page.getByText('Sign in to view your balances.')).toBeVisible();
    release(); await page.goto(`${base}/#/holds`);
    await expect(page).toHaveURL(/login/); await expect(page.getByRole('article')).toHaveCount(0);
  } finally { release(); }
});
