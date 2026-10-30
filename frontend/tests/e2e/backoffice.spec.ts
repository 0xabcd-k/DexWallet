import { test, expect, type Page } from '@playwright/test';
import { envelope, userIdx } from './auth-fixture';

function token(realm: string, version: number) {
  const exp = Math.floor(Date.now() / 1000) + 259200;
  return `e30.${Buffer.from(JSON.stringify({ iss: 'dexwallet_test', aud: [`${realm}-user`], sub: userIdx, auth_version: version, iat: exp - 259200, exp })).toString('base64url')}.test-signature`;
}
async function mockBackoffice(page: Page, realm: string) {
  const state = {
    version: 1,
    mustChangePassword: true,
    password: 'Temporary-password-123',
    failures: 0,
    loseCreateReply: false,
    merchants: [] as {
      idx: string;
      email: string;
      status: string;
      mustChangePassword: boolean;
    }[],
  };
  await page.route(`**/api/v1/${realm}/**`, async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.split(`/${realm}`)[1];
    const input = req.postDataJSON() ?? {};
    const fail = (status: number, code: string) =>
      route.fulfill({
        status,
        json: { code, message: 'test error', traceId: 'backoffice-test' },
      });
    const ok = (data: unknown) => route.fulfill({ json: envelope(data) });
    const issue = () => {
      const raw = token(realm, state.version);
      return route.fulfill({
        headers: { 'X-Access-Token': raw },
        json: envelope({
          accessToken: raw,
          userIdx,
          expiresAt: JSON.parse(
            Buffer.from(raw.split('.')[1]!, 'base64url').toString(),
          ).exp,
        }),
      });
    };
    if (path === '/auth/code') {
      if (input.password !== state.password) {
        state.failures++;
        return fail(400, 'AUTH_PROOF_INVALID');
      }
      return ok({
        challenge: 'a'.repeat(64),
        expiresAt: Math.floor(Date.now() / 1000) + 300,
      });
    }
    if (path === '/auth/login') {
      if (input.password !== state.password || input.code !== '123456') {
        state.failures++;
        return fail(400, 'AUTH_PROOF_INVALID');
      }
      return issue();
    }
    let claims;
    try {
      claims = JSON.parse(
        Buffer.from(
          req.headers().authorization!.split('.')[1]!,
          'base64url',
        ).toString(),
      );
    } catch {
      return fail(401, 'UNAUTHENTICATED');
    }
    if (
      claims.auth_version !== state.version ||
      claims.aud[0] !== `${realm}-user`
    )
      return fail(401, 'UNAUTHENTICATED');
    if (path === '/current')
      return ok({
        idx: userIdx,
        email: `${realm}@example.com`,
        status: 'active',
        mustChangePassword: state.mustChangePassword,
      });
    if (path === '/password') {
      if (input.oldPassword !== state.password)
        return fail(400, 'AUTH_PROOF_INVALID');
      state.password = input.password;
      state.mustChangePassword = false;
      state.version++;
      return issue();
    }
    if (state.mustChangePassword) return fail(403, 'PERMISSION_DENIED');
    if (path === '/accounts') return ok({ items: state.merchants });
    if (path === '/merchants') {
      const row = {
        idx: '01234567890123457',
        email: input.email,
        status: 'active',
        mustChangePassword: true,
      };
      state.merchants.push(row);
      if (state.loseCreateReply) return fail(500, 'INTERNAL_ERROR');
      return ok(row);
    }
    if (path === '/audits') return ok({ items: [{
      id: 1, actorRealm: 'admin', actorIdx: userIdx, action: 'change_status',
      targetRealm: 'merchant', targetIdx: userIdx, targetEmail: 'shop@example.com',
      beforeValue: 'active', afterValue: 'disabled', reason: 'manual reason',
      result: 'success', traceId: 'audit-test', createdAt: 1700000000,
    }] });
    return fail(404, 'NOT_FOUND');
  });
  return state;
}
for (const [realm, port] of [
  ['admin', 5175],
  ['merchant', 5174],
] as const) {
  test(`${realm}: password plus email code, forced change, persistence and logout`, async ({
    page,
  }) => {
    const state = await mockBackoffice(page, realm);
    await page.goto(`http://127.0.0.1:${port}/settings`);
    await expect(
      page.getByRole('heading', {
        name: realm === 'admin' ? 'Platform sign in' : 'Merchant sign in',
      }),
    ).toBeVisible();
    await page
      .getByLabel('Email', { exact: true })
      .fill(`${realm}@example.com`);
    await page.getByLabel('Password', { exact: true }).fill('wrong-password');
    await page
      .getByRole('button', { name: 'Verify password and send code' })
      .click();
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page.getByLabel('Email verification code')).toHaveCount(0);
    await page.getByLabel('Password', { exact: true }).fill(state.password);
    await page
      .getByRole('button', { name: 'Verify password and send code' })
      .click();
    await page.getByLabel('Email verification code').fill('000000');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('alert')).toBeVisible();
    await page.getByLabel('Email verification code').fill('123456');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(
      page.getByText(
        'Change your temporary password before using backoffice features.',
      ),
    ).toBeVisible();
    await page.getByLabel('Current password').fill(state.password);
    await page.getByLabel('New password').fill('Changed-password-456');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByRole('status')).toHaveText('Saved.');
    await expect(
      page.getByText(
        'Change your temporary password before using backoffice features.',
      ),
    ).toHaveCount(0);
    await page.reload();
    await expect(
      page.getByRole('heading', { name: 'Settings', exact: true }),
    ).toBeVisible();
    expect(state.failures).toBe(2);
    await expect(
      page.getByRole('button', { name: /all devices/i }),
    ).toHaveCount(0);
    if (realm === 'admin') {
      await page.goto(`http://127.0.0.1:${port}/accounts`);
      await page
        .getByRole('button', { name: 'Create merchant account' })
        .click();
      await page.getByLabel('Email', { exact: true }).fill('new@example.com');
      await page.getByLabel('Temporary password').fill('New-merchant-password');
      await page.getByLabel('Reason', { exact: true }).fill('onboard');
      await page.getByRole('button', { name: 'Confirm', exact: true }).click();
      await expect(
        page.getByRole('cell', { name: 'new@example.com' }),
      ).toBeVisible();
      expect(state.merchants).toHaveLength(1);
      await page.goto(`http://127.0.0.1:${port}/audits`);
      await expect(
        page.getByRole('heading', { name: 'Activity log' }),
      ).toBeVisible();
      await page.goto(`http://127.0.0.1:${port}/settings`);
    }
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await page.reload();
    await expect(
      page.getByRole('button', { name: 'Verify password and send code' }),
    ).toBeVisible();
  });
  test(`${realm}: wrong-audience saved credential and external return path are rejected`, async ({
    page,
  }) => {
    await mockBackoffice(page, realm);
    await page.addInitScript(
      ({ realm, raw }) =>
        localStorage.setItem(
          `dexwallet:${realm}:session`,
          JSON.stringify({ generation: 'seed', token: raw }),
        ),
      { realm, raw: token(realm === 'admin' ? 'merchant' : 'admin', 1) },
    );
    await page.goto(
      `http://127.0.0.1:${port}/login?returnTo=https://example.com/`,
    );
    await expect(
      page.getByRole('button', { name: 'Verify password and send code' }),
    ).toBeVisible();
    await page
      .getByLabel('Email', { exact: true })
      .fill(`${realm}@example.com`);
    await page
      .getByLabel('Password', { exact: true })
      .fill('Temporary-password-123');
    await page
      .getByRole('button', { name: 'Verify password and send code' })
      .click();
    await page.getByLabel('Email verification code').fill('123456');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page).toHaveURL(`http://127.0.0.1:${port}/settings`);
  });
}

test('admin: uncertain create result guides verification and audit states follow language', async ({ page }) => {
  const state = await mockBackoffice(page, 'admin');
  state.mustChangePassword = false;
  state.loseCreateReply = true;
  await page.addInitScript(raw => localStorage.setItem('dexwallet:admin:session',
    JSON.stringify({ generation: 'seed', token: raw })), token('admin', 1));
  await page.goto('http://127.0.0.1:5175/accounts');
  await page.getByRole('button', { name: 'Create merchant account' }).click();
  await page.getByLabel('Email', { exact: true }).fill('uncertain@example.com');
  await page.getByLabel('Temporary password').fill('New-password-123');
  await page.getByLabel('Reason', { exact: true }).fill('onboard');
  await page.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Check the current state or sign in again before retrying');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'uncertain@example.com' })).toBeVisible();
  expect(state.merchants).toHaveLength(1);
  await page.goto('http://127.0.0.1:5175/audits');
  await page.getByRole('button', { name: 'Expand row' }).click();
  await expect(page.getByText('Before: Active', { exact: true })).toBeVisible();
  await expect(page.getByText('After: Disabled', { exact: true })).toBeVisible();
  await page.getByRole('combobox').selectOption('zh-CN');
  await expect(page.getByText('变更前: 正常', { exact: true })).toBeVisible();
  await expect(page.getByText('变更后: 已禁用', { exact: true })).toBeVisible();
  await expect(page.getByText('操作原因: manual reason', { exact: true })).toBeVisible();
});
