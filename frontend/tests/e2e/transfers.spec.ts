import { test, expect, type Page } from '@playwright/test';
import { envelope, mockAccount, seedSession, userIdx } from './auth-fixture';
const base = 'http://127.0.0.1:5173';
const recipientIdx = '09876543210987654';
const result = { idx: '01111111111111111', counterparty: { idx: recipientIdx, nickname: 'Recipient', avatarUrl: '' }, amount: '10.00', fee: '0.00', direction: 'out', unit: 'USD', status: 'completed', createdAt: '2026-11-13T00:00:00Z' };
const policy = { plan: { idx: '02222222222222222', name: 'Default', minAmount: '0.01', maxAmount: '100.00', dailyOutgoingLimit: '500.00' }, usedToday: '0.00', remainingToday: '500.00', resetAt: new Date(Date.now() + 86400000).toISOString(), restrictedUntil: '' };
async function fixture(page: Page) {
  await mockAccount(page); await seedSession(page);
  await page.route('**/api/v1/user/transfer-policy', route => route.fulfill({ json: envelope(policy) }));
  await page.route('**/api/v1/user/transfer-targets/resolve', route => route.fulfill({ json: envelope(result.counterparty) }));
  await page.route('**/api/v1/user/accounts', route => route.fulfill({ json: envelope({ accounts: [
    { kind: 'usd_balance', unit: 'USD', balance: '20.00', heldBalance: '0.00', totalBalance: '20.00' },
    { kind: 'reward_usd', unit: 'USD', balance: '0.00', heldBalance: '0.00', totalBalance: '0.00' },
    { kind: 'task_points', unit: 'POINT', balance: '0', heldBalance: '0', totalBalance: '0' },
  ] }) }));
}
async function review(page: Page, amount = '10') {
  await page.goto(`${base}/#/pay?manual=1`);
  await page.getByLabel('Recipient ID', { exact: true }).fill(recipientIdx);
  await page.getByLabel('Transfer amount (USD)').fill(amount);
  await page.getByRole('button', { name: 'Review payment', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Review payment' })).toBeVisible();
}
test.use({ viewport: { width: 390, height: 844 } });
test('payment requires explicit review, persists before sending and recovers lost response without resending', async ({ page }) => {
  await fixture(page); let writes = 0; let submittedKey = ''; let visible = false;
  await page.route('**/api/v1/user/transfers', async route => {
    writes++; const input = route.request().postDataJSON() as { requestKey: string; recipientIdx: string; amount: string };
    submittedKey = input.requestKey;
    const saved = await page.evaluate(idx => JSON.parse(localStorage.getItem(`dexwallet:user:pending-transfer:dexwallet_test:${idx}`)!), userIdx);
    expect(saved).toEqual(input); visible = true; await route.abort();
  });
  await page.route('**/api/v1/user/transfers/by-request/*', route => route.fulfill(visible ? { json: envelope(result) } : { status: 404, json: { code: 'NOT_FOUND', message: 'absent', traceId: 'test' } }));
  await review(page); expect(writes).toBe(0);
  await expect(page.locator('.payment-review')).toContainText(recipientIdx);
  await page.screenshot({ path: `test-results/payment-review-${test.info().project.name}.png`, fullPage: true });
  await expect(page.getByRole('button', { name: 'Confirm payment', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Confirm payment', exact: true }).click();
  await expect(page.getByText('Payment awaiting confirmation')).toBeVisible();
  expect(submittedKey.length).toBeGreaterThanOrEqual(16);
  await page.reload(); await expect(page.getByText('Transfer completed')).toBeVisible(); expect(writes).toBe(1);
  expect(await page.evaluate(idx => localStorage.getItem(`dexwallet:user:pending-transfer:dexwallet_test:${idx}`), userIdx)).toBeNull();
});
test('unknown query is not proof of failure and retry keeps the original key', async ({ page }) => {
  await fixture(page); const keys: string[] = []; let first = true;
  await page.route('**/api/v1/user/transfers', route => { keys.push(route.request().postDataJSON().requestKey); if (first) { first = false; return route.abort(); } return route.fulfill({ json: envelope(result) }); });
  await page.route('**/api/v1/user/transfers/by-request/*', route => route.fulfill({ status: 404, json: { code: 'NOT_FOUND', message: 'absent', traceId: 'test' } }));
  await review(page); await page.getByRole('button', { name: 'Confirm payment', exact: true }).click();
  await page.reload(); await expect(page.getByText('The original result is unconfirmed.', { exact: false })).toBeVisible(); expect(keys).toHaveLength(1);
  await page.getByRole('button', { name: 'Review original payment' }).click();
  await page.getByRole('button', { name: 'Confirm payment', exact: true }).click();
  await expect(page.getByText('Transfer completed')).toBeVisible(); expect(keys).toHaveLength(2); expect(keys[1]).toBe(keys[0]);
});
test('insufficient balance and binding restriction keep confirmation disabled', async ({ page }) => {
  await fixture(page); await review(page, '21');
  await expect(page.getByText('Insufficient balance', { exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Confirm payment', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.route('**/api/v1/user/transfer-policy', route => route.fulfill({ json: envelope({ ...policy, restrictedUntil: new Date(Date.now() + 86400000).toISOString() }) }));
  await page.getByLabel('Transfer amount (USD)').fill('10'); await page.getByRole('button', { name: 'Review payment', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Confirm payment', exact: true })).toBeDisabled();
  await expect(page.getByText('Payments are restricted after identity replacement until the time below.')).toBeVisible();
});
test('receive page renders a reusable QR without amount and personal links reject fixed amounts', async ({ page }) => {
  await fixture(page); await page.route('**/api/v1/user/receive-code', route => route.fulfill({ json: envelope({ user: { idx: userIdx, nickname: 'Payer', avatarUrl: '' }, code: 'sample-code', url: 'https://wallet.example/#/pay?code=sample-code', telegramUrl: '' }) }));
  await page.goto(`${base}/#/receive`); await expect(page.getByRole('img', { name: 'Personal receive QR code' })).toBeVisible();
  await expect(page.getByText('This reusable code has no fixed amount.', { exact: false })).toBeVisible();
  await expect(page.getByLabel('Transfer amount (USD)')).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: `test-results/receive-code-${test.info().project.name}.png`, fullPage: true });
  await page.goto(`${base}/#/pay?code=sample-code&amount=10`); await expect(page.getByRole('alert')).toContainText('Personal codes do not support fixed amounts');
});
test('replacement warning can be cancelled without sending identity mutation', async ({ page }) => {
  const state = await mockAccount(page); state.telegram = true; await seedSession(page); let mutations = 0;
  page.on('request', r => { if (r.url().endsWith('/identities/email/verify')) mutations++; });
  await page.goto(`${base}/#/settings`); await page.getByRole('button', { name: 'Replace', exact: true }).first().click();
  await page.getByLabel('Email', { exact: true }).fill('New@example.com'); await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456'); await page.getByRole('button', { name: 'Verify and link' }).click();
  await expect(page.getByText('Replacing this identity pauses payments', { exact: false })).toBeVisible(); await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  expect(mutations).toBe(0); expect(state.email).toBe('Alice@example.com');
});

test('email login returns to scanned target and never submits automatically', async ({ page }) => {
  await mockAccount(page); let submissions = 0;
  page.on('request', r => { if (r.url().endsWith('/user/transfers') && r.method() === 'POST') submissions++; });
  await page.goto(`${base}/#/pay?code=signed-code`);
  await page.getByLabel('Email', { exact: true }).fill('Alice@example.com'); await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456'); await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/pay?code=signed-code`); await expect(page.getByLabel('Receive link or code')).toHaveValue('signed-code'); expect(submissions).toBe(0);
});

test('Telegram payment launch preserves the code through silent login without sending funds', async ({ page }) => {
  await mockAccount(page); let writes = 0;
  page.on('request', r => { if (r.url().endsWith('/user/transfers') && r.method() === 'POST') writes++; });
  const code = Buffer.from(JSON.stringify([1, 'account', recipientIdx, 'fake-signature-for-routing-only'])).toString('base64url');
  const init = new URLSearchParams({ user: JSON.stringify({ id: 7, first_name: 'Test' }), start_param: code, auth_date: `${Math.floor(Date.now() / 1000)}`, hash: 'b'.repeat(64), signature: 'unverified-fixture' });
  const launch = new URLSearchParams({ tgWebAppData: init.toString(), tgWebAppVersion: '8.0', tgWebAppPlatform: 'web', tgWebAppThemeParams: '{}' });
  await page.goto(`${base}/#${launch}`); await expect(page).toHaveURL(`${base}/#/pay?code=${code}`);
  await expect(page.getByLabel('Receive link or code')).toHaveValue(code); expect(writes).toBe(0);
});

test('changing the scanned target discards an in-flight payment review', async ({ page }) => {
  await fixture(page); let release: () => void = () => {}; let started: () => void = () => {};
  const blocked = new Promise<void>(resolve => { release = resolve; }); const reading = new Promise<void>(resolve => { started = resolve; });
  await page.route('**/api/v1/user/transfer-targets/resolve', async route => { started(); await blocked; await route.fulfill({ json: envelope(result.counterparty) }).catch(() => {}); });
  try {
    await page.goto(`${base}/#/pay?manual=1`); await page.getByLabel('Recipient ID', { exact: true }).fill(recipientIdx); await page.getByLabel('Transfer amount (USD)').fill('10');
    await page.getByRole('button', { name: 'Review payment', exact: true }).click(); await reading;
    await page.evaluate(() => { location.hash = '/pay?code=new-receive-code'; }); await expect(page.getByLabel('Receive link or code')).toHaveValue('new-receive-code');
    release(); await expect(page.getByRole('button', { name: 'Review payment', exact: true })).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Confirm payment', exact: true })).toHaveCount(0);
  } finally { release(); }
});
