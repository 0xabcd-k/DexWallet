import { webcrypto } from 'node:crypto';
import { beforeAll, it, expect, vi } from 'vitest';
import { bootstrapTelegram, updateTarget, LAUNCH_STORAGE_KEY } from '../../apps/user/src/app/telegram/bootstrap';
import { safeReturn } from '../../apps/user/src/app/navigation';
beforeAll(() => { Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true }); });
function launch(target?: string, nonce = 'one') {
  const init = new URLSearchParams({ user: JSON.stringify({ id: 1, language_code: 'zh-Hans' }), auth_date: '1700000000', hash: nonce });
  if (target !== undefined) init.set('start_param', target);
  return new URLSearchParams({ tgWebAppData: init.toString(), tgWebAppVersion: '8.0', tgWebAppPlatform: 'web' }).toString();
}
function boot(raw: string, href = `https://example.test/#${raw}`, navigationType = 'navigate') {
  return bootstrapTelegram({ href, navigationType, storage: sessionStorage, retrieve: () => raw });
}
it('captures before routing and only persists lifecycle metadata', async () => {
  const raw = launch('invite_abc'); const result = await boot(raw);
  expect(result).toMatchObject({ mode: 'telegram', source: 'fresh', target: 'invite_abc', targetStatus: 'pending', language: 'zh-Hans' });
  const saved = sessionStorage.getItem(LAUNCH_STORAGE_KEY)!;
  expect(saved).not.toContain('auth_date'); expect(saved).not.toContain('rawInitData');
});
it('preserves consumed targets on refresh and restores the same launch', async () => {
  const raw = launch('invite_abc'); const first = updateTarget(await boot(raw), 'consumed', sessionStorage);
  const restored = await boot(raw, 'https://example.test/#/records?filter=a', 'reload');
  expect(restored).toMatchObject({ source: 'restored', launchId: first.launchId, targetStatus: 'consumed' });
});
it('treats every fresh launch separately, including identical target values', async () => {
  const raw = launch('same'); const first = updateTarget(await boot(raw), 'consumed', sessionStorage);
  const second = await boot(raw);
  expect(second.launchId).not.toBe(first.launchId); expect(second.targetStatus).toBe('pending');
  expect(await boot(launch())).toMatchObject({ target: null, targetStatus: 'consumed' });
});
it('does not recover old SDK caches in ordinary browser visits', async () => {
  await boot(launch('old')); const retrieve = vi.fn(() => launch('old'));
  const result = await bootstrapTelegram({ href: 'https://example.test/#/wallet', navigationType: 'navigate', storage: sessionStorage, retrieve });
  expect(result.mode).toBe('browser'); expect(retrieve).not.toHaveBeenCalled();
});
it('rejects mismatched caches and malformed fresh launches without exposing raw data', async () => {
  await boot(launch('old'));
  expect((await boot(launch('new', 'two'), 'https://example.test/#/wallet', 'reload')).mode).toBe('error');
  expect((await boot(launch('old'), 'https://example.test/#tgWebAppVersion=8.0')).mode).toBe('error');
  const incomplete = new URLSearchParams(launch('old')); incomplete.delete('tgWebAppPlatform');
  expect((await boot(launch('old'), `https://example.test/#${incomplete}`)).mode).toBe('error');
});
it('rejects contradictory or invalid target hints', async () => {
  const raw = launch('one');
  expect(await boot(raw, `https://example.test/?tgWebAppStartParam=two#${raw}`)).toMatchObject({ target: null, targetStatus: 'invalid' });
  expect((await boot(launch('bad/path'))).targetStatus).toBe('invalid');
});
it('can capture a fresh launch in memory when lifecycle storage is unavailable', async () => {
  const raw = launch('one');
  expect(await bootstrapTelegram({ href: `https://example.test/#${raw}`, navigationType: 'navigate', storage: undefined, retrieve: () => raw })).toMatchObject({ mode: 'telegram', storageAvailable: false });
});
it('restricts child-page return targets to known local parents', () => {
  expect(safeReturn({ from: '/records?filter=a&page=2' })).toBe('/records?filter=a&page=2');
  for (const from of ['//evil.test', 'https://evil.test', '/settings', '/wallet\\evil', '/wallet#x']) expect(safeReturn({ from })).toBe('/wallet');
  expect(safeReturn(null)).toBe('/wallet');
});
