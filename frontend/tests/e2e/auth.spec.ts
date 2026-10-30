import { test, expect } from '@playwright/test';
import { browserToken, envelope, mockAccount, seedSession, SESSION_KEY, telegramLaunch, userIdx } from './auth-fixture';
const base = 'http://127.0.0.1:5173';
test.use({ viewport: { width: 390, height: 844 } });

test('email login, errors, source recovery, profile, reload and logout', async ({ page }) => {
  await mockAccount(page);
  await page.goto(`${base}/#/account`);
  await expect(page).toHaveURL(/\/login\?returnTo=%2Faccount/);
  await page.getByLabel('Email', { exact: true }).fill('Alice@example.com');
  await page.getByRole('combobox').selectOption('zh-CN');
  await expect(page.getByLabel('邮箱', { exact: true })).toHaveValue('Alice@example.com');
  await page.getByRole('combobox').selectOption('en');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await expect(page.getByRole('button', { name: /Resend in/ })).toBeDisabled();
  await page.getByLabel('Verification code', { exact: true }).fill('000000');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('invalid or expired');
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/settings`);
  await expect(page.getByText(userIdx, { exact: true })).toBeVisible();
  await page.getByLabel('Nickname', { exact: true }).fill('My product name');
  await page.getByRole('button', { name: 'Save nickname' }).click();
  await expect(page.getByRole('status')).toContainText('has been saved');
  await page.reload(); await expect(page.getByLabel('Nickname')).toHaveValue('My product name');
  await expect(page.getByRole('button', { name: 'Replace', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Unlink', exact: true })).toBeDisabled();
  await page.screenshot({ path: `test-results/identities-${test.info().project.name}.png`, fullPage: true });
  await expect(page.getByRole('button', { name: 'Switch account', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sign out everywhere', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/wallet`);
  expect(await page.evaluate(key => JSON.parse(localStorage.getItem(key)!).token, SESSION_KEY)).toBeNull();
});

test('Telegram launch signs in once and hides account switching, logout and Telegram replacement', async ({ page }) => {
  const state = await mockAccount(page);
  await page.goto(`${base}/#${telegramLaunch()}`);
  await expect(page).toHaveURL(`${base}/#/wallet`);
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await expect(page.getByText('Telegram Test', { exact: false })).toBeVisible();
  expect(state.tokenRequests).toHaveLength(1);
  state.email = 'Alice@example.com';
  await page.reload();
  await expect(page.getByText(state.email, { exact: true })).toBeVisible();
  const telegram = page.locator('.settings-identity').filter({ has: page.getByRole('heading', { name: 'Telegram', exact: true }) });
  await expect(telegram.getByRole('button', { name: 'Replace', exact: true })).toHaveCount(0);
  await expect(telegram.getByRole('button', { name: 'Unlink', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Replace', exact: true })).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Account actions', exact: true })).toHaveCount(0);
  for (const name of ['Switch account', 'Sign out', 'Sign out everywhere']) {
    await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0);
  }
  expect(state.tokenRequests).toHaveLength(1);
  state.unavailable = true;
  await page.reload();
  await expect(page.getByRole('alert')).toContainText('temporarily unavailable');
  await expect(page.getByRole('button', { name: 'Try again', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toHaveCount(0);
});

test('binding conflict keeps account; success returns to source and new version survives replacement', async ({ page }) => {
  const state = await mockAccount(page); await seedSession(page);
  await page.goto(`${base}/#${telegramLaunch()}`);
  await page.goto(`${base}/#/identities?required=telegram&returnTo=%2Frewards`);
  await expect(page.getByRole('status')).toContainText('Link Telegram');
  await page.getByRole('button', { name: 'Link', exact: true }).click();
  state.occupied = true;
  await page.getByRole('button', { name: 'Use this Telegram identity' }).click();
  await expect(page.getByRole('alert')).toContainText('another account');
  expect(await page.evaluate(key => JSON.parse(localStorage.getItem(key)!).token !== null, SESSION_KEY)).toBe(true);
  state.occupied = false;
  await page.getByRole('button', { name: 'Use this Telegram identity' }).click();
  await expect(page).toHaveURL(`${base}/#/rewards`);
  await page.goto(`${base}/#/identities`);
  await page.getByRole('button', { name: 'Replace', exact: true }).first().click();
  await page.getByLabel('Email', { exact: true }).fill('New@example.com');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Verify and link' }).click();
  await expect(page).toHaveURL(`${base}/#/settings`);
  await expect(page.getByText('New@example.com')).toBeVisible();
  await page.reload(); await expect(page.getByText(userIdx, { exact: true })).toBeVisible();
});

test('503 retains credentials, 401 requests login and never follows external return targets', async ({ page }) => {
  const state = await mockAccount(page); await seedSession(page);
  state.unavailable = true;
  await page.goto(`${base}/#/account`);
  await expect(page.getByRole('alert')).toContainText('temporarily unavailable');
  expect(await page.evaluate(key => JSON.parse(localStorage.getItem(key)!).token !== null, SESSION_KEY)).toBe(true);
  state.unavailable = false;
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByText(userIdx, { exact: true })).toBeVisible();
  state.version++;
  await page.reload(); await expect(page.getByRole('link', { name: 'Sign in', exact: true })).toBeVisible();
  await page.goto(`${base}/#/login?returnTo=https%3A%2F%2Fevil.example`);
  await page.getByLabel('Email', { exact: true }).fill('Alice@example.com');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(`${base}/#/settings`);
});

test('late renewal cannot restore logout, and another tab observes logout', async ({ page, context }) => {
  await mockAccount(page); await seedSession(page); await page.goto(`${base}/#/account`);
  await expect(page.getByText(userIdx, { exact: true })).toBeVisible();
  const other = await context.newPage(); await mockAccount(other); await other.goto(`${base}/#/account`);
  await expect(other.getByText(userIdx, { exact: true })).toBeVisible();
  let release: () => void = () => {}; const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/v1/user/current', async route => { await held; await route.fulfill({ headers: { 'X-Access-Token': browserToken(1, Math.floor(Date.now() / 1000) + 259300) }, json: envelope({ idx: userIdx, nickname: 'Changed', avatarUrl: '' }) }).catch(() => {}); });
  await page.getByLabel('Nickname').fill('Changed'); await page.getByRole('button', { name: 'Save nickname' }).click();
  await expect(page.getByRole('status')).toContainText('has been saved');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click(); release();
  await expect(page).toHaveURL(`${base}/#/wallet`); await expect(other.getByRole('link', { name: 'Sign in', exact: true })).toBeVisible();
  expect(await page.evaluate(key => JSON.parse(localStorage.getItem(key)!).token, SESSION_KEY)).toBeNull();
});

test('lost replacement response retries the original operation with the original token', async ({ page }) => {
  const state = await mockAccount(page); state.telegram = true; const originalToken = browserToken(); await seedSession(page, originalToken);
  let originalRequest = ''; let calls = 0;
  await page.route('**/api/v1/user/identities/email/verify', async route => {
    calls++; const input: Record<string, string> = route.request().postDataJSON();
    expect(route.request().headers().authorization).toBe(`Bearer ${originalToken}`);
    if (calls === 1) { originalRequest = input.requestToken!; state.version++; state.email = input.email!; return route.abort(); }
    expect(input.requestToken).toBe(originalRequest);
    const raw = browserToken(state.version); const expiresAt = JSON.parse(Buffer.from(raw.split('.')[1]!, 'base64url').toString()).exp;
    return route.fulfill({ json: envelope({ accessToken: raw, userIdx, expiresAt }) });
  });
  await page.goto(`${base}/#/identities`);
  await page.getByRole('button', { name: 'Replace', exact: true }).first().click();
  await page.getByLabel('Email', { exact: true }).fill('Recovered@example.com');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Verify and link' }).click();
  await expect(page.getByRole('alert')).toContainText('Unable to connect');
  await page.getByRole('button', { name: 'Verify and link' }).click();
  await expect(page).toHaveURL(`${base}/#/settings`);
  await expect(page.getByText('Recovered@example.com')).toBeVisible(); expect(calls).toBe(2);
});

test('blocked browser storage still allows memory-only email login', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('blocked', 'SecurityError'); } });
    Object.defineProperty(navigator, 'locks', { value: { request() { return Promise.reject(new DOMException('blocked', 'SecurityError')); } } });
  });
  await mockAccount(page); await page.goto(`${base}/#/account`);
  await expect(page.getByText('Sign-in is kept only on this page', { exact: false })).toBeVisible();
  await page.getByLabel('Email', { exact: true }).fill('Alice@example.com');
  await page.getByRole('button', { name: 'Send code', exact: true }).click();
  await page.getByLabel('Verification code', { exact: true }).fill('123456');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByText(userIdx, { exact: true })).toBeVisible();
  await page.reload(); await expect(page.getByRole('link', { name: 'Sign in', exact: true })).toBeVisible();
});
