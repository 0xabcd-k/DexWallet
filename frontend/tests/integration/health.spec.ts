import { test, expect } from '@playwright/test';
import { spawn, execFileSync, type ChildProcess } from 'node:child_process';
import { openSync, closeSync } from 'node:fs';
import { once } from 'node:events';
import { join } from 'node:path';
const dir = process.env.DEXWALLET_INTEGRATION_DIR;
if (!dir) throw new Error('Run pnpm test:integration to prepare an isolated API process.');
let backend: ChildProcess | undefined;
async function start() {
  execFileSync(join(dir!, 'testenv'), ['check', 'http'], { stdio: 'pipe' });
  const log = openSync(join(dir!, 'api.log'), 'a', 0o600);
  try { backend = spawn(join(dir!, 'api'), ['-f', join(dir!, 'platform.json')], { stdio: ['ignore', log, log] }); }
  finally { closeSync(log); }
  await expect.poll(async () => {
    if (backend?.exitCode !== null) throw new Error('API exited; inspect the HTTP evidence api.log');
    try { return (await fetch('http://127.0.0.1:18888/health/live', { signal: AbortSignal.timeout(1000) })).status; }
    catch { return 0; }
  }, { timeout: 15_000 }).toBe(200);
}
async function stop() {
  const child = backend; backend = undefined;
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, 'exit'); child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 45_000);
  try { const [code, signal] = await exited; expect(code).toBe(0); expect(signal).toBeNull(); } finally { clearTimeout(timer); }
}
test.beforeAll(start);
test.afterAll(stop);
test('real API through Vite proxy: success, stopped API, explicit retry after restart', async ({ page }) => {
  await page.goto('http://127.0.0.1:5173/#/__dev');
  await page.getByRole('button', { name: 'Check API connection' }).click();
  await expect(page.getByRole('status')).toContainText('API connection available');
  await stop();
  await page.getByRole('button', { name: 'Check API connection' }).click();
  await expect(page.getByRole('alert')).toContainText('temporarily unavailable');
  await start();
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('status')).toContainText('API connection available');
});

for (const [index, app] of ['user', 'merchant', 'admin'].entries()) {
  for (const mode of ['dev', 'preview']) {
    test(`${app} ${mode}: real health, trace, route refresh and HTTP errors`, async ({ page }) => {
      const port = (mode === 'dev' ? 5173 : 4173) + index;
      const route = app === 'user' ? '/#/wallet' : '/overview';
      await page.goto(`http://127.0.0.1:${port}${route}`);
      const ready = app === 'user' ? page.getByRole('navigation') : page.getByLabel('Email', { exact: true });
      await expect(ready).toBeVisible();
      if (app !== 'user') await expect(page).toHaveURL(/\/login/);
      await page.reload();
      await expect(ready).toBeVisible();
      for (const endpoint of ['live', 'ready']) {
        const result = await page.evaluate(async endpoint => {
          const response = await fetch(`/api/health/${endpoint}`);
          return { status: response.status, trace: response.headers.get('x-trace-id'), body: await response.json() };
        }, endpoint);
        expect(result.status).toBe(200);
        expect(result.body.code).toBe('OK');
        expect(result.body.traceId).toBeTruthy();
        expect(result.trace).toBe(result.body.traceId);
      }
      const missing = await page.evaluate(async () => {
        const response = await fetch('/api/does-not-exist');
        return { status: response.status, type: response.headers.get('content-type'), body: await response.text() };
      });
      expect(missing.status).toBe(404); expect(missing.type).toContain('text/plain');
      expect(missing.body).toContain('404');
      // Prefix matching must not hijack an ordinary SPA route.
      const ordinary = await page.request.get(`http://127.0.0.1:${port}/apix`);
      expect(ordinary.headers()['content-type']).toContain('text/html');
    });
  }
}
