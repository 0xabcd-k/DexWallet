import { test, expect, type Page } from '@playwright/test';
import { envelope, mockAccount, seedSession } from './auth-fixture';
const base = 'http://127.0.0.1:5173';
const versionIdx = '04444444444444444';
const result = { idx: '03333333333333333', versionIdx, versionName: 'candidate_v1', costPoints: '100', prizeCode: 'ordinary_one', rewardUsd: '0.08', symbols: [1,5,6], voucherIdx: '05555555555555555', createdAt: '2026-11-15T00:00:00Z' };
const rules = ['ordinary_one','ordinary_two','ordinary_three','small','double','large','top'].map((code,i) => ({ code, rewardUsd: ['0.08','0.09','0.10','0.12','0.20','1.00','5.00'][i], weight: [1500,5000,3000,400,90,9,1][i], symbols: [[1,5,6],[1,1,5],[1,1,1],[2,2,2],[3,3,3],[4,4,4],[7,7,7]][i] }));
async function fixture(page: Page, points = 100) {
  await mockAccount(page); await seedSession(page);
  const state = { points, posted: 0, completed: false, versionIdx, keys: [] as string[] };
  await page.route('**/api/v1/user/draw-state', route => route.fulfill({ json: envelope({ available: true, versionIdx: state.versionIdx, versionName: 'candidate_v1', costPoints: '100', rtp: '0.95', prizes: rules, points: String(state.points), rewardUsd: state.completed ? '0.08' : '0.00' }) }));
  await page.route('**/api/v1/user/draws', route => { state.posted++; state.keys.push(route.request().postDataJSON().requestKey); state.points -= 100; state.completed = true; return route.fulfill({ json: envelope(result) }); });
  await page.route('**/api/v1/user/draws?*', route => route.fulfill({ json: envelope({ items: state.completed ? [result] : [], nextCursor: '' }) }));
  await page.route(`**/api/v1/user/draws/${result.idx}`, route => route.fulfill({ json: envelope(result) }));
  return state;
}
test.use({ viewport: { width: 390, height: 844 } });
test('draws once, shows seven probabilities, and reopens a settled record without drawing again', async ({ page }) => {
  const s = await fixture(page); await page.goto(`${base}/#/draws`); expect(s.posted).toBe(0);
  await page.getByText('Prize rules and probabilities', { exact: true }).click();
  await expect(page.getByRole('row')).toHaveCount(8); await expect(page.getByText('0.01%', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Draw once for 100 points' }).click();
  await expect(page.getByRole('heading', { name: 'Draw settled' })).toBeVisible();
  await expect(page.getByText('Ordinary I · $0.08')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Draw once for 100 points' })).toBeDisabled();
  expect(s.posted).toBe(1); expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: `test-results/draw-${test.info().project.name}.png`, fullPage: true, animations: 'disabled' });
  await page.getByRole('link', { name: 'My draw history' }).click(); await expect(page.getByRole('heading', { name: 'Draw settled' })).toBeVisible();
  await page.reload(); expect(s.posted).toBe(1); await expect(page.locator('.draw-reveal')).toHaveCount(0);
});
test('insufficient points disables the draw and directs the user to tasks', async ({ page }) => {
  const s = await fixture(page, 37); await page.goto(`${base}/#/draws`);
  await expect(page.getByRole('button', { name: 'Draw once for 100 points' })).toBeDisabled();
  await expect(page.getByText('You need 63 more points.', { exact: false })).toBeVisible(); expect(s.posted).toBe(0);
});
test('lost response and reload retain the original key; a lookup miss never starts another draw', async ({ page }) => {
  const s = await fixture(page); let visible = false; let key = '';
  await page.route('**/api/v1/user/draws', route => { s.posted++; key = route.request().postDataJSON().requestKey; return route.abort('failed'); });
  await page.route('**/api/v1/user/draws/by-request/*', route => { expect(route.request().url()).toContain(key); return route.fulfill(visible ? { json: envelope(result) } : { status: 404, json: { code: 'NOT_FOUND', message: 'Not found', traceId: 'test' } }); });
  await page.goto(`${base}/#/draws`); await page.getByRole('button', { name: 'Draw once for 100 points' }).click();
  await expect(page.getByRole('button', { name: 'Check original draw' })).toBeVisible(); await page.reload();
  await page.getByRole('button', { name: 'Check original draw' }).click(); await expect(page.getByRole('button', { name: 'Retry original draw' })).toBeVisible(); expect(s.posted).toBe(1);
  visible = true; await page.getByRole('button', { name: 'Check original draw' }).click();
  await expect(page.getByRole('heading', { name: 'Draw settled' })).toBeVisible(); expect(s.posted).toBe(1); await expect(page.locator('.draw-reveal')).toHaveCount(0);
});
test('version changes require another explicit click after showing the new rules', async ({ page }) => {
  const s = await fixture(page); const newVersion = '06666666666666666';
  await page.route('**/api/v1/user/draws', route => { s.posted++; s.versionIdx = newVersion; return route.fulfill({ status: 409, json: { code: 'DRAW_VERSION_CHANGED', message: 'Changed', traceId: 'test' } }); });
  await page.route('**/api/v1/user/draws/by-request/*', route => route.fulfill({ status: 404, json: { code: 'NOT_FOUND', message: 'Missing', traceId: 'test' } }));
  await page.goto(`${base}/#/draws`); await page.getByRole('button', { name: 'Draw once for 100 points' }).click();
  await expect(page.getByText('Prize rules changed.', { exact: false })).toBeVisible(); await expect(page.getByRole('button', { name: 'Draw once for 100 points' })).toBeEnabled(); expect(s.posted).toBe(1);
});
test('two tabs prepare a single pending draw and share its recovery', async ({ page, context }) => {
  const first = await fixture(page); const secondPage = await context.newPage(); const second = await fixture(secondPage);
  await page.route('**/api/v1/user/draws', route => { first.posted++; return route.abort('failed'); });
  await secondPage.route('**/api/v1/user/draws', route => { second.posted++; return route.abort('failed'); });
  await Promise.all([page.goto(`${base}/#/draws`), secondPage.goto(`${base}/#/draws`)]);
  await Promise.all([page.getByRole('button', { name: 'Draw once for 100 points' }).click(), secondPage.getByRole('button', { name: 'Draw once for 100 points' }).click()]);
  await expect(page.getByRole('button', { name: 'Check original draw' })).toBeVisible(); await expect(secondPage.getByRole('button', { name: 'Check original draw' })).toBeVisible(); expect(first.posted + second.posted).toBe(1);
});
test('a concurrent balance decrease refreshes the shortfall after a definitive refusal', async ({ page }) => {
  const s = await fixture(page);
  await page.route('**/api/v1/user/draws', route => { s.posted++; s.points = 20; return route.fulfill({ status: 422, json: { code: 'INSUFFICIENT_BALANCE', message: 'Insufficient', traceId: 'test' } }); });
  await page.route('**/api/v1/user/draws/by-request/*', route => route.fulfill({ status: 404, json: { code: 'NOT_FOUND', message: 'Missing', traceId: 'test' } }));
  await page.goto(`${base}/#/draws`); await page.getByRole('button', { name: 'Draw once for 100 points' }).click();
  await expect(page.getByText('You need 80 more points.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Draw once for 100 points' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Retry original draw' })).toHaveCount(0); expect(s.posted).toBe(1);
});
