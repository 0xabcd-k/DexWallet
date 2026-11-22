import { test, expect, type Page } from '@playwright/test';
import { mockAccount, seedSession, envelope, userIdx } from './auth-fixture';
const admin = 'http://127.0.0.1:5175';
const user = 'http://127.0.0.1:5173';
const taskIdx = '02234567890123456';
const versionIdx = '03234567890123456';
async function adminSession(page: Page) { const exp = Math.floor(Date.now() / 1000) + 259200; const token = `e30.${Buffer.from(JSON.stringify({ iss: 'dexwallet_test', aud: ['admin-user'], sub: userIdx, auth_version: 1, iat: exp - 259200, exp })).toString('base64url')}.test-signature`; await page.addInitScript(token => { if (!localStorage.getItem('dexwallet:admin:session'))
    localStorage.setItem('dexwallet:admin:session', JSON.stringify({ generation: 'ops-admin', token })); }, token); }
const current = { idx: userIdx, email: 'admin@example.com', status: 'active', mustChangePassword: false };
test('invitation results filter, paginate, and link only to own grant', async ({ page }) => {
    await mockAccount(page);
    await seedSession(page);
    await page.route('**/api/v1/user/reward-summary', route => route.fulfill({ json: envelope({
        firstCompleted: true, proProgress: '1000', pro: false, proUnlockedAt: '',
        friendCount: 32, assistanceCount: 41, assistancePoints: '9007199254740993', commissionPoints: '287',
    }) }));
    const queries: URLSearchParams[] = [];
    await page.route('**/api/v1/user/invitation-earnings?*', route => { const p = new URL(route.request().url()).searchParams; queries.push(p); return route.fulfill({ json: envelope({ items: [{ grantIdx: versionIdx, peerIdx: userIdx, peerNickname: 'Invited friend', reason: 'commission', points: '20', taskTitle: p.get('before') ? 'Older task' : 'New task', createdAt: '2026-11-15T00:00:00Z' }], nextCursor: p.get('before') ? '' : versionIdx }) }); });
    await page.route('**/api/v1/user/referral-friends?*', route => route.fulfill({ json: envelope({ items: [{ userIdx, nickname: 'Invited friend', boundAt: '2026-11-14T00:00:00Z' }], nextCursor: '' }) }));
    await page.goto(`${user}/#/invitations`);
    const totals = page.getByRole('region', { name: 'Lifetime totals' }).getByRole('definition');
    const expectedTotals = ['32', '41', '☆9007199254740993', '☆287'];
    await expect(totals).toHaveText(expectedTotals);
    await expect(page.getByText('New task', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Next page' }).click();
    await expect(page.getByText('Older task', { exact: true })).toBeVisible();
    await expect(totals).toHaveText(expectedTotals);
    await page.getByRole('tab', { name: 'Friends', exact: true }).click();
    await page.getByRole('button', { name: 'View earnings from friend' }).click();
    await expect(page.getByLabel('Friend idx (optional)')).toHaveValue(userIdx);
    await page.getByLabel('Reward type').selectOption('commission');
    await page.getByLabel('From date (UTC)').fill('2026-11-01');
    await page.getByLabel('Through date (UTC)').fill('2026-11-15');
    await page.getByRole('button', { name: 'Apply filters' }).click();
    await expect.poll(() => queries.at(-1)?.get('from')).toBe('2026-11-01');
    expect(queries.at(-1)?.get('before')).toBe('');
    expect(queries.at(-1)?.get('friendIdx')).toBe(userIdx);
    await expect(totals).toHaveText(expectedTotals);
    await expect(page.getByRole('link', { name: 'View completion record' })).toHaveAttribute('href', `#/reward-grants/${versionIdx}`);
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: `test-results/invitation-results-${test.info().project.name}.png`, fullPage: true });
});
test('task draft recovers the same request after lost response and reload, publishes with revision', async ({ page }) => {
    await adminSession(page);
    let stored: Record<string, unknown> | undefined;
    const requests: Record<string, unknown>[] = [];
    let statuses = 0;
    await page.route('**/api/v1/admin/**', route => {
        const path = new URL(route.request().url()).pathname;
        const req = route.request();
        if (path.endsWith('/current'))
            return route.fulfill({ json: envelope(current) });
        if (path.endsWith('/task-drafts')) {
            const body = req.postDataJSON();
            requests.push(body);
            stored = { ...body, idx: taskIdx, status: 'draft', revision: 1 };
            return requests.length === 1 ? route.abort('failed') : route.fulfill({ json: envelope(stored) });
        }
        if (path.endsWith('/status')) {
            statuses++;
            expect(req.postDataJSON().expectedRevision).toBe(1);
            stored = { ...stored, status: 'active', revision: 2 };
            return route.fulfill({ json: envelope(stored) });
        }
        if (path.endsWith('/completions'))
            return route.fulfill({ json: envelope({ items: [], nextCursor: '' }) });
        return route.fulfill({ json: envelope({ items: stored ? [stored] : [], nextCursor: '', writable: true }) });
    });
    await page.goto(`${admin}/tasks`);
    await page.getByRole('button', { name: 'New draft' }).click();
    const form = page.locator('form');
    await form.getByLabel('Title', { exact: true }).fill('Mock follow');
    await form.getByLabel('Description', { exact: true }).fill('Local channel example');
    await form.getByLabel('Base points', { exact: true }).fill('9007199254740993');
    await form.getByLabel('Reason', { exact: true }).fill('Mock validation');
    await form.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Retry original request' })).toBeVisible();
    await page.reload();
    await expect(page.getByRole('button', { name: 'Retry original request' })).toBeVisible();
    await expect(page.getByLabel('Title', { exact: true })).toBeDisabled();
    await page.getByRole('button', { name: 'Retry original request' }).click();
    await expect(page.getByRole('button', { name: 'Retry original request' })).toHaveCount(0);
    expect(requests).toHaveLength(2);
    expect(requests[1]).toEqual(requests[0]);
    expect(requests[0]?.basePoints).toBe('9007199254740993');
    await page.getByLabel('Reason', { exact: true }).fill('Publish sample');
    await page.getByRole('button', { name: 'Publish / resume' }).click();
    await expect(page.getByRole('button', { name: 'Pause', exact: true })).toBeVisible();
    expect(statuses).toBe(1);
    await page.getByRole('button', { name: 'Preview', exact: true }).click();
    await expect(page.getByLabel('Title', { exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Save', exact: true })).toHaveCount(0);
    await page.getByRole('button', { name: 'Completion records', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Completion records' })).toBeVisible();
    await page.screenshot({ path: `test-results/task-operations-${test.info().project.name}.png`, fullPage: true });
});
test('draw version save stays paused until activation and stale control never retries silently', async ({ page }) => {
    await adminSession(page);
    let version: Record<string, unknown> | undefined;
    let active = '';
    let revision = 1;
    let controls = 0;
    let conflict = true;
    await page.route('**/api/v1/admin/**', route => {
        const path = new URL(route.request().url()).pathname;
        const request = route.request();
        if (path.endsWith('/current'))
            return route.fulfill({ json: envelope(current) });
        if (path.endsWith('/draw-versions') && request.method() === 'POST') {
            version = { ...request.postDataJSON(), idx: versionIdx };
            return route.fulfill({ json: envelope(version) });
        }
        if (path.endsWith('/draw-control')) {
            controls++;
            expect(request.postDataJSON().expectedRevision).toBe(revision);
            if (conflict) {
                conflict = false;
                revision++;
                return route.fulfill({ status: 409, json: { code: 'CONFLICT', message: 'Changed', traceId: 'ops' } });
            }
            active = request.postDataJSON().versionIdx;
            revision++;
        }
        return route.fulfill({ json: envelope({ items: version ? [version] : [], currentVersionIdx: active, revision, writable: true, nextCursor: '' }) });
    });
    await page.goto(`${admin}/draw-versions`);
    await page.getByRole('button', { name: 'Create version' }).click();
    const form = page.locator('form');
    await form.getByLabel('Version name (unique)').fill('browser_candidate');
    await form.getByLabel('Reason', { exact: true }).fill('Candidate verification');
    await form.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Activate version' })).toBeVisible();
    expect(active).toBe('');
    expect(controls).toBe(0);
    await page.getByLabel('Reason', { exact: true }).fill('Activate candidate');
    await page.getByRole('button', { name: 'Activate version' }).click();
    await expect(page.locator('[role="alert"]')).toBeVisible();
    expect(controls).toBe(1);
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await page.getByRole('button', { name: 'Activate version' }).click();
    await expect(page.getByRole('button', { name: 'Activate version' })).toBeDisabled();
    expect(active).toBe(versionIdx);
    await page.getByRole('button', { name: 'Pause', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Pause', exact: true })).toBeDisabled();
    expect(active).toBe('');
});
