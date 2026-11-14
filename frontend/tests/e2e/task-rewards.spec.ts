import { test, expect, type Page } from '@playwright/test';
import { envelope, mockAccount, seedSession } from './auth-fixture';
const base = 'http://127.0.0.1:5173';
const taskIdx = '03333333333333333'; const invitationIdx = '04444444444444444'; const completionIdx = '05555555555555555';
const task = { idx: taskIdx, kind: 'telegram_channel_follow', title: 'Reward sample', description: 'No real channel subscription needed.', requiredIdentity: 'telegram', startsAt: '2026-01-01T00:00:00Z', endsAt: '2027-01-01T00:00:00Z', eligibility: 'eligible', sample: true, claimAvailable: true, rewardsAvailable: true };
const firstGrant = { idx: '06666666666666666', taskIdx, completionIdx, taskTitle: task.title, reason: 'first_task', rewardUsd: '1.00', points: '0', proDelta: '1000', proBefore: '0', proAfter: '1000', wasPro: false, unlockedPro: false, voucherIdx: '07777777777777777', createdAt: '2026-11-14T00:00:00Z' };
const completed = { idx: completionIdx, taskIdx, taskTitle: task.title, mode: 'self', participantStage: 'first', completedAt: '2026-11-14T00:00:00Z', grants: [firstGrant] };
async function fixture(page: Page, help = false) {
  await mockAccount(page); await seedSession(page);
  const state = { completed: false, stage: 'first', posts: 0 };
  const item = () => ({ ...task, eligibility: state.completed ? 'completed' : 'eligible', claimAvailable: !state.completed });
  await page.route(`**/api/v1/user/tasks/${taskIdx}`, route => route.fulfill({ json: envelope(item()) }));
  await page.route(`**/api/v1/user/tasks/${taskIdx}/claim-preview?*`, route => route.fulfill({ json: envelope({ task: item(), mode: help ? 'help' : 'self', participantStage: state.stage, taskRevision: 1, identityRevision: 1, invitationIdx: help ? invitationIdx : '', inviterIdx: help ? '08888888888888888' : '', inviterNickname: help ? 'Inviter Jane' : '', rewardUsd: state.stage === 'first' ? '1.00' : '0.00', taskPoints: help || state.stage === 'first' ? '0' : '100', commissionPoints: '0', proDelta: state.stage === 'first' ? '1000' : '0', proProgress: state.stage === 'first' ? '0' : '1000', pro: false, canInvite: state.stage === 'returning' }) }));
  await page.route(`**/api/v1/user/tasks/${taskIdx}/claim`, route => { state.posts++; state.completed = true; state.stage = 'returning'; return route.fulfill({ json: envelope({ ...completed, mode: help ? 'help' : 'self' }) }); });
  await page.route('**/api/v1/user/task-completions?*', route => route.fulfill({ json: envelope({ items: state.completed ? [completed] : [], nextCursor: '' }) }));
  await page.route(`**/api/v1/user/task-completions/${completionIdx}`, route => route.fulfill({ json: envelope(completed) }));
  return state;
}
test.use({ viewport: { width: 390, height: 844 } });
test('first claim settles reward cash and Pro without spendable points, then history survives reload', async ({ page }) => {
  const state = await fixture(page); await page.goto(`${base}/#/tasks/${taskIdx}`);
  await expect(page.getByText('Your first completed task awards $1', { exact: false })).toBeVisible();
  expect(state.posts).toBe(0);
  await page.getByRole('button', { name: 'Check and claim rewards', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Task completed and rewards settled' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'First task reward' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Invite a friend to this task' })).toBeVisible();
  expect(state.posts).toBe(1);
  await expect(page.getByRole('heading', { name: 'Estimated rewards for you' })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: `test-results/task-claim-${test.info().project.name}.png`, fullPage: true });
  await page.reload(); await page.getByRole('link', { name: 'My completed tasks' }).click();
  await expect(page.getByRole('heading', { name: 'Task completed and rewards settled' })).toBeVisible();
  expect(state.posts).toBe(1);
});
test('old help requires explicit consent and never claims on opening an invitation', async ({ page }) => {
  const state = await fixture(page, true); state.stage = 'returning';
  await page.route(`**/api/v1/user/tasks/${taskIdx}/claim`, route => { state.posts++; return route.fulfill({ json: envelope({ ...completed, mode: 'help', participantStage: 'returning', grants: [] }) }); });
  await page.goto(`${base}/#/tasks/${taskIdx}?invitationIdx=${invitationIdx}`);
  const button = page.getByRole('button', { name: 'Confirm help and claim' });
  await expect(button).toBeDisabled(); expect(state.posts).toBe(0);
  await expect(page.getByText('Helping Inviter Jane')).toBeVisible();
  await page.getByRole('checkbox').check(); await button.click();
  await expect(page.getByText('Your help was completed and this attempt was used.', { exact: false })).toBeVisible();
  expect(state.posts).toBe(1);
});
test('first-task context changes require a fresh help confirmation', async ({ page }) => {
  const state = await fixture(page, true);
  await page.route(`**/api/v1/user/tasks/${taskIdx}/claim`, route => { state.posts++; state.stage = 'returning'; return route.fulfill({ status: 409, json: { code: 'TASK_CONTEXT_CHANGED', message: 'Changed', traceId: 'test' } }); });
  await page.route('**/api/v1/user/task-completions/by-request/*', route => route.fulfill({ status: 404, json: { code: 'NOT_FOUND', message: 'Not found', traceId: 'test' } }));
  await page.goto(`${base}/#/tasks/${taskIdx}?invitationIdx=${invitationIdx}`);
  await page.getByRole('checkbox').check(); await page.getByRole('button', { name: 'Confirm help and claim' }).click();
  await expect(page.getByRole('checkbox')).not.toBeChecked();
  await expect(page.getByRole('button', { name: 'Confirm help and claim' })).toBeDisabled();
  await expect(page.getByText('The task reward goes to the inviter;', { exact: false })).toBeVisible();
  expect(state.posts).toBe(1);
});
test('lost response keeps the same request through reload and lookup misses', async ({ page }) => {
  const state = await fixture(page); let requestKey = ''; let visible = false;
  await page.route(`**/api/v1/user/tasks/${taskIdx}/claim`, route => { state.posts++; requestKey = (route.request().postDataJSON() as { requestKey: string }).requestKey; return route.abort('failed'); });
  await page.route('**/api/v1/user/task-completions/by-request/*', route => {
    expect(route.request().url()).toContain(requestKey);
    return route.fulfill(visible ? { json: envelope(completed) } : { status: 404, json: { code: 'NOT_FOUND', message: 'Not found', traceId: 'test' } });
  });
  await page.goto(`${base}/#/tasks/${taskIdx}`); await page.getByRole('button', { name: 'Check and claim rewards' }).click();
  await expect(page.getByRole('button', { name: 'Check original result' })).toBeVisible();
  await page.reload(); await page.getByRole('button', { name: 'Check original result' }).click();
  await expect(page.getByText('A previous claim needs checking.', { exact: false })).toBeVisible();
  expect(state.posts).toBe(1); visible = true;
  await page.getByRole('button', { name: 'Check original result' }).click();
  await expect(page.getByRole('heading', { name: 'Task completed and rewards settled' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Check original result' })).toHaveCount(0);
  expect(state.posts).toBe(1);
});
test('task invitation landing resolves a specific task without automatically claiming', async ({ page }) => {
  const state = await fixture(page, true);
  await page.route(`**/api/v1/user/task-invitations/${invitationIdx}`, route => route.fulfill({ json: envelope({ idx: invitationIdx, taskIdx, inviterIdx: '08888888888888888', inviterNickname: 'Inviter Jane', inviterAvatarUrl: '', path: 'https://untrusted.example/', startParam: `task_${invitationIdx}`, url: '', telegramUrl: '' }) }));
  await page.goto(`${base}/#/task-invitations/${invitationIdx}`);
  await expect(page).toHaveURL(new RegExp(`/tasks/${taskIdx}\\?invitationIdx=${invitationIdx}$`));
  await expect(page.getByRole('button', { name: 'Confirm help and claim' })).toBeDisabled();
  expect(state.posts).toBe(0);
});
