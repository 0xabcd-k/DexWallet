import type { Page } from '@playwright/test';
export const userIdx = '01234567890123456';
export const SESSION_KEY = 'dexwallet:user:session';
export function browserToken(version = 1, expiresAt = Math.floor(Date.now() / 1000) + 259200) {
  return `e30.${Buffer.from(JSON.stringify({ iss: 'dexwallet_test', aud: ['product-user'], sub: userIdx, auth_version: version, iat: expiresAt - 259200, exp: expiresAt })).toString('base64url')}.test-signature`;
}
export function telegramLaunch() {
  const init = new URLSearchParams({ user: JSON.stringify({ id: 7, first_name: 'Telegram Test', language_code: 'en' }), auth_date: `${Math.floor(Date.now() / 1000)}`, hash: 'b'.repeat(64), signature: 'synthetic-unverified' });
  return new URLSearchParams({ tgWebAppData: init.toString(), tgWebAppVersion: '8.0', tgWebAppPlatform: 'web', tgWebAppThemeParams: '{}' }).toString();
}
export function envelope(data: unknown) { return { code: 'OK', message: 'OK', traceId: 'browser-auth-trace', data }; }
export async function mockAccount(page: Page) {
  const state = { version: 1, nickname: '', email: 'Alice@example.com', telegram: false, unavailable: false, occupied: false, tokenRequests: [] as string[] };
  await page.route('**/api/v1/user/**', async route => {
    const req = route.request(); const path = new URL(req.url()).pathname.replace('/api', '');
    const input: Record<string, string> = req.postDataJSON() ?? {};
    const fail = (status: number, code: string) => route.fulfill({ status, json: { code, message: 'test error', traceId: 'browser-auth-trace' } });
    const ok = (data: unknown) => route.fulfill({ json: envelope(data) });
    const issue = () => {
      state.tokenRequests.push(path);
      const raw = browserToken(state.version);
      return route.fulfill({ headers: { 'X-Access-Token': raw, 'Cache-Control': 'no-store' }, json: envelope({ accessToken: raw, userIdx, expiresAt: JSON.parse(Buffer.from(raw.split('.')[1]!, 'base64url').toString()).exp }) });
    };
    if (state.unavailable) return fail(503, 'SERVICE_UNAVAILABLE');
    if (path === '/v1/user/auth/email/code') return ok({ challenge: 'a'.repeat(64), expiresAt: Math.floor(Date.now() / 1000) + 300 });
    if (path === '/v1/user/auth/email/login') {
      if (input.code !== '123456') return fail(400, 'AUTH_PROOF_INVALID');
      state.email = input.email ?? ''; return issue();
    }
    if (path === '/v1/user/auth/telegram/login') { state.telegram = true; state.email = ''; return issue(); }
    let version: number | undefined;
    try { const raw = req.headers().authorization?.split(' ')[1]?.split('.')[1]; if (raw) version = JSON.parse(Buffer.from(raw, 'base64url').toString()).auth_version; } catch { /* invalid header */ }
    if (version !== state.version) return fail(401, 'UNAUTHENTICATED');
    if (path === '/v1/user/current') return ok({ idx: userIdx, nickname: state.nickname, avatarUrl: '' });
    if (path === '/v1/user/accounts') return ok({ accounts: [
      { kind: 'usd_balance', unit: 'USD', balance: '0.00', heldBalance: '0.00', totalBalance: '0.00' },
      { kind: 'reward_usd', unit: 'USD', balance: '0.00', heldBalance: '0.00', totalBalance: '0.00' },
      { kind: 'task_points', unit: 'POINT', balance: '0', heldBalance: '0', totalBalance: '0' },
    ] });
    if (path === '/v1/user/identities') return ok({ identities: [
      ...(state.email ? [{ type: 'email', subject: state.email, displayName: state.email }] : []),
      ...(state.telegram ? [{ type: 'telegram', subject: '7', displayName: 'Telegram Test' }] : []),
    ] });
    if (path === '/v1/user/profile') { state.nickname = input.nickname ?? ''; return ok({ idx: userIdx, nickname: state.nickname, avatarUrl: '' }); }
    if (path === '/v1/user/identities/email/code') return ok({ challenge: 'b'.repeat(64), expiresAt: Math.floor(Date.now() / 1000) + 300 });
    if (path.endsWith('/verify')) {
      if (state.occupied) return fail(409, 'IDENTITY_OCCUPIED');
      if (input.purpose === 'replace') state.version++;
      if (path.includes('/email/')) { if (input.code !== '123456') return fail(400, 'AUTH_PROOF_INVALID'); state.email = input.email ?? ''; }
      else state.telegram = true;
      return issue();
    }
    if (path.endsWith('/unbind')) { state.version++; if (input.identity === 'email') state.email = ''; else state.telegram = false; return issue(); }
    return fail(404, 'NOT_FOUND');
  });
  return state;
}
export async function seedSession(page: Page, token = browserToken()) {
  await page.addInitScript(({ key, token }) => { if (!localStorage.getItem(key)) localStorage.setItem(key, JSON.stringify({ generation: 'seed-generation', token })); }, { key: SESSION_KEY, token });
}
