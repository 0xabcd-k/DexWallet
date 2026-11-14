import { test, expect, type Page } from '@playwright/test';
import { envelope, mockAccount, seedSession } from './auth-fixture';
const base = 'http://127.0.0.1:5173';
const taskIdx = '03333333333333333';
const sample = { idx: taskIdx, kind: 'telegram_channel_follow', title: 'Sample channel', description: 'No real channel subscription is needed.', requiredIdentity: 'telegram', startsAt: '2026-01-01T00:00:00Z', endsAt: '2027-01-01T00:00:00Z', eligibility: 'eligible', sample: true, claimAvailable: false };
async function fixture(page: Page, eligibility = 'eligible') {
  const account = await mockAccount(page); account.telegram = eligibility !== 'telegram_required';
  await seedSession(page);
  const item = { ...sample, eligibility };
  await page.route('**/api/v1/user/tasks?*', route => route.fulfill({ json: envelope({ items: [item], nextCursor: '' }) }));
  await page.route(`**/api/v1/user/tasks/${taskIdx}`, route => route.fulfill({ json: envelope(item) }));
  return item;
}
test.use({ viewport: { width: 390, height: 844 } });
test('task check is explicit, repeatable and never represents a reward or real subscription', async ({ page }) => {
  await fixture(page); let checks = 0; const writes: string[] = [];
  page.on('request', req => { if (req.method() === 'POST') writes.push(new URL(req.url()).pathname); });
  await page.route(`**/api/v1/user/tasks/${taskIdx}/check`, route => { checks++; return route.fulfill({ json: envelope({ task: sample, outcome: 'member', checkedAt: new Date().toISOString() }) }); });
  await page.goto(`${base}/#/tasks`);
  await page.getByRole('link', { name: 'Sample channel' }).click();
  await expect(page.getByText('Sample task: membership is simulated.', { exact: false })).toBeVisible();
  expect(checks).toBe(0);
  await page.getByRole('button', { name: 'Check membership', exact: true }).click();
  await expect(page.getByText('Follow condition met', { exact: true })).toBeVisible();
  await expect(page.getByText('Reward claiming is not available yet.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Check membership', exact: true }).click();
  await expect.poll(() => checks).toBe(2);
  expect(writes.every(path => path.endsWith('/check'))).toBe(true);
  expect(await page.locator('a[href^="https://t.me/"]').count()).toBe(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: `test-results/task-preview-${test.info().project.name}.png`, fullPage: true });
  await page.reload(); await expect(page.getByRole('button', { name: 'Check membership' })).toBeVisible();
  expect(checks).toBe(2); await expect(page.getByText('Follow condition met', { exact: true })).toHaveCount(0);
});
test('unbound users are guided to identity settings and cannot verify', async ({ page }) => {
  await fixture(page, 'telegram_required'); await page.goto(`${base}/#/tasks/${taskIdx}`);
  await expect(page.getByRole('button', { name: 'Check membership' })).toBeDisabled();
  await page.getByRole('link', { name: 'Bind Telegram' }).click();
  await expect(page).toHaveURL(/#\/settings$/);
  await page.getByRole('link', { name: 'Back', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${taskIdx}$`));
});
test('nonmember and verification failure have distinct results and stale success is cleared', async ({ page }) => {
  await fixture(page); let fail = false;
  await page.route(`**/api/v1/user/tasks/${taskIdx}/check`, route => route.fulfill(fail
    ? { status: 503, json: { code: 'TASK_VERIFICATION_UNAVAILABLE', message: 'mock timeout', traceId: 'test' } }
    : { json: envelope({ task: sample, outcome: 'not_member', checkedAt: new Date().toISOString() }) }));
  await page.goto(`${base}/#/tasks/${taskIdx}`);
  await page.getByRole('button', { name: 'Check membership' }).click();
  await expect(page.getByText('Not following in this test scenario', { exact: true })).toBeVisible();
  fail = true; await page.getByRole('button', { name: 'Check membership' }).click();
  await expect(page.getByRole('alert')).toContainText('Unable to verify membership now. No attempt was consumed.');
  await expect(page.getByText('Not following in this test scenario', { exact: true })).toHaveCount(0);
});
test('expired and already used eligibility disable checking', async ({ page }) => {
  for (const eligibility of ['expired', 'completed']) {
    await fixture(page, eligibility); await page.goto(`${base}/#/tasks/${taskIdx}`); await page.reload();
    await expect(page.getByRole('button', { name: 'Check membership' })).toBeDisabled();
    await expect(page.getByText(eligibility === 'expired' ? 'This task has expired.' : 'This Telegram account has already used its attempt.', { exact: true })).toBeVisible();
  }
});
