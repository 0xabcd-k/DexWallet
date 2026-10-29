import { test, expect } from '@playwright/test';
import { mockAccount } from './auth-fixture';
const health = { code: 'OK', message: 'OK', traceId: 'browser-trace', data: { status: 'live' } };
for (const [index, app] of ['user', 'merchant', 'admin'].entries()) {
  const base = `http://127.0.0.1:${5173 + index}`;
  const prefix = app === 'user' ? '/#' : '';
  test(`${app}: route, language, input, failure and recovery`, async ({ page }) => {
    await page.setViewportSize(app === 'user' ? { width: 390, height: 844 } : { width: 1440, height: 900 });
    await page.goto(`${base}${prefix}/__dev?filter=initial&page=2`);
    await expect(page.getByRole('heading', { name: 'Development checks' })).toBeVisible();
    await page.getByLabel('Sample input', { exact: true }).fill('preserved draft');
    await page.getByRole('combobox').last().selectOption('zh-CN');
    await expect(page.getByLabel('示例输入', { exact: true })).toHaveValue('preserved draft');
    await expect(page.getByLabel('筛选', { exact: true })).toHaveValue('initial');
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
    await page.getByRole('combobox').last().selectOption('en');
    await page.route('**/api/health/live', route => route.fulfill({ status: 503, contentType: 'text/html', body: 'proxy failure' }));
    await page.getByRole('button', { name: 'Check API connection' }).click();
    await expect(page.getByRole('alert')).toContainText('temporarily unavailable');
    await page.unroute('**/api/health/live');
    await page.route('**/api/health/live', route => route.fulfill({ json: health }));
    await page.getByRole('button', { name: 'Try again' }).click();
    await expect(page.getByRole('status')).toContainText('API connection available');
    await expect(page.getByText('Amount: 12,345,678,901,234,567,890.12345678 USDT')).toBeVisible();
    if (app === 'user') {
      await page.getByRole('link', { name: 'Interaction sample' }).click();
      await expect(page.getByRole('navigation')).toHaveCount(0);
      await page.getByRole('link', { name: 'Back', exact: true }).click();
      await expect(page).toHaveURL(`${base}/#/__dev?filter=initial&page=2`);
    }
    await page.reload();
    await expect(page.getByLabel('Filter', { exact: true })).toHaveValue('initial');
    await expect(page.getByLabel('Sample input', { exact: true })).toHaveValue('');
    await page.goto(`${base}${prefix}/missing`);
    await expect(page.getByText('Page not found', { exact: true })).toBeVisible();
    await page.getByRole('link', { name: 'Home', exact: true }).click();
    await expect(page.getByRole('navigation')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: `test-results/${app}-${test.info().project.name}.png`, fullPage: true });
  });
  test(`${app}: production entry excludes development route`, async ({ page }) => {
    const production = `http://127.0.0.1:${4173 + index}`;
    await page.goto(`${production}${prefix}/__dev`);
    await expect(page.getByText('Page not found', { exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Development checks' })).toHaveCount(0);
    await page.getByRole('link', { name: 'Home', exact: true }).click();
    await expect(page.getByRole('navigation')).toBeVisible();
  });
}
test('user: direct child link has a safe fallback and invalid pagination is normalized', async ({ page }) => {
  await page.goto('http://127.0.0.1:5173/#/settings');
  await page.getByRole('link', { name: 'Back', exact: true }).click();
  await expect(page).toHaveURL('http://127.0.0.1:5173/#/wallet');
  await page.goto('http://127.0.0.1:5173/#/__dev?page=-1');
  await expect(page.getByText('Page: 1', { exact: true })).toBeVisible();
  await page.getByLabel('Sample input', { exact: true }).focus();
  await expect(page.getByLabel('Sample input', { exact: true })).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.getByLabel('Filter', { exact: true })).toBeFocused();
});
test('user: cancellation leaves no failure alert and the next request can succeed', async ({ page }) => {
  let release: (() => void) | undefined;
  const blocked = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/health/live', async route => { await blocked; await route.fulfill({ json: health }).catch(() => {}); });
  await page.goto('http://127.0.0.1:5173/#/__dev');
  await page.getByRole('button', { name: 'Check API connection' }).click();
  await expect(page.getByRole('status')).toHaveText('Loading…');
  await page.getByRole('button', { name: 'Cancel request' }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  release?.();
  await page.unroute('**/api/health/live');
  await page.route('**/api/health/live', route => route.fulfill({ json: health }));
  await page.getByRole('button', { name: 'Check API connection' }).click();
  await expect(page.getByRole('status')).toContainText('API connection available');
});
test('user: native request deadline aborts a stalled fetch', async ({ page }) => {
  let release: () => void = () => {};
  const blocked = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/health/live', async route => {
    await blocked;
    await route.fulfill({ json: health }).catch(() => {});
  });
  try {
    await page.goto('http://127.0.0.1:5173/#/wallet');
    const kind = await page.evaluate(async () => {
      const modulePath = '/src/api/request.ts';
      const { api }: { api: import('@dexwallet/api-client').ApiClient } = await import(modulePath);
      try {
        await api.request('/health/live', value => value, { timeoutMs: 25 });
        return 'unexpected-success';
      } catch (error) {
        return error instanceof Error && 'kind' in error ? error.kind : 'unexpected-error';
      }
    });
    expect(kind).toBe('timeout');
  } finally {
    release();
  }
});
test('user: route load failure displays a recoverable localized boundary', async ({ page }) => {
  await page.route('**/src/view/dev/index.tsx', route => route.abort());
  await page.goto('http://127.0.0.1:5173/#/__dev');
  await expect(page.getByRole('heading', { name: 'Unable to display this page' })).toBeVisible();
  await page.getByRole('link', { name: 'Home', exact: true }).click();
  await expect(page.getByRole('navigation')).toBeVisible();
});
function launch(target?: string) {
  const init = new URLSearchParams({ user: JSON.stringify({ id: 1, first_name: 'Synthetic', language_code: 'en' }), auth_date: '1700000000', signature: 'synthetic-unverified', hash: 'a'.repeat(64) });
  if (target) init.set('start_param', target);
  return new URLSearchParams({ tgWebAppData: init.toString(), tgWebAppVersion: '8.0', tgWebAppPlatform: 'web', tgWebAppThemeParams: '{}' }).toString();
}
test('user: real SDK captures synthetic launch before Hash Router and restores refresh', async ({ page }) => {
  await mockAccount(page);
  await page.goto(`http://127.0.0.1:5173/#${launch('invite_test')}`);
  await expect(page).toHaveURL('http://127.0.0.1:5173/#/wallet');
  const original = await page.evaluate(() => JSON.parse(sessionStorage.getItem('dexwallet:user:launch') ?? 'null'));
  expect(original.target).toBe('invite_test');
  expect(original.targetStatus).toBe('pending');
  await page.getByRole('link', { name: 'Records', exact: true }).click();
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await page.getByRole('combobox').selectOption('zh-CN');
  await page.getByRole('link', { name: '返回', exact: true }).click();
  expect(await page.evaluate(() => JSON.parse(sessionStorage.getItem('dexwallet:user:launch')!).launchId)).toBe(original.launchId);
  await page.evaluate(() => { const key = 'dexwallet:user:launch'; const saved = JSON.parse(sessionStorage.getItem(key)!); saved.targetStatus = 'consumed'; sessionStorage.setItem(key, JSON.stringify(saved)); });
  await page.reload();
  await expect(page).toHaveURL('http://127.0.0.1:5173/#/records');
  const restored = await page.evaluate(() => JSON.parse(sessionStorage.getItem('dexwallet:user:launch') ?? 'null'));
  expect(restored.launchId).toBe(original.launchId); expect(restored.targetStatus).toBe('consumed');
  // Full navigation simulates a newly launched document, not a hash-only SPA navigation.
  await page.goto('about:blank');
  await page.goto(`http://127.0.0.1:5173/#${launch()}`);
  await expect(page).toHaveURL('http://127.0.0.1:5173/#/wallet');
  const fresh = await page.evaluate(() => JSON.parse(sessionStorage.getItem('dexwallet:user:launch') ?? 'null'));
  expect(fresh.launchId).not.toBe(original.launchId); expect(fresh.target).toBeNull();
});
test('user: unavailable storage allows browser use but explicitly rejects unrecoverable SDK launch', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('blocked', 'SecurityError'); } });
    Object.defineProperty(window, 'sessionStorage', { get() { throw new DOMException('blocked', 'SecurityError'); } });
  });
  await page.goto('http://127.0.0.1:5173/#/settings');
  await page.getByRole('combobox').selectOption('zh-CN');
  await expect(page.getByRole('heading', { name: '设置', exact: true })).toBeVisible();
  await page.goto('about:blank');
  await page.goto(`http://127.0.0.1:5173/#${launch('candidate')}`);
  await expect(page.getByText('Unable to restore this Telegram session. Reopen the app from Telegram.')).toBeVisible();
});
test.describe('touch navigation', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });
  test('user: narrow touch navigation remains usable', async ({ page }) => {
    await page.goto('http://127.0.0.1:5173/#/wallet');
    await page.getByRole('link', { name: 'Records', exact: true }).tap();
    await expect(page).toHaveURL('http://127.0.0.1:5173/#/records');
    await page.getByRole('link', { name: 'Settings', exact: true }).tap();
    await page.getByRole('link', { name: 'Back', exact: true }).tap();
    await expect(page).toHaveURL('http://127.0.0.1:5173/#/records');
  });
});
