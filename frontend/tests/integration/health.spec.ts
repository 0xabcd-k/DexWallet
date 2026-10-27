import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { openSync, closeSync } from 'node:fs';
import { once } from 'node:events';
import { join } from 'node:path';
const dir = process.env.DEXWALLET_INTEGRATION_DIR;
if (!dir) throw new Error('Run pnpm test:integration to prepare an isolated API process.');
let backend: ChildProcess | undefined;
async function start() {
  const log = openSync(join(dir!, 'api.log'), 'a', 0o600);
  try { backend = spawn(join(dir!, 'api'), ['-f', join(dir!, 'platform.yaml')], { stdio: ['ignore', log, log] }); }
  finally { closeSync(log); }
  await expect.poll(async () => {
    if (backend?.exitCode !== null) throw new Error('API exited; inspect .tools/frontend-integration/api.log');
    try { return (await fetch('http://127.0.0.1:18888/health/live', { signal: AbortSignal.timeout(1000) })).status; }
    catch { return 0; }
  }, { timeout: 15_000 }).toBe(200);
}
async function stop() {
  const child = backend; backend = undefined;
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, 'exit'); child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
  try { await exited; } finally { clearTimeout(timer); }
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
