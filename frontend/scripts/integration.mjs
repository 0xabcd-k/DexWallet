import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { createServer } from 'node:net';
const root = fileURLToPath(new URL('../../', import.meta.url));
let child;
let interrupted = false;
function signalGroup(signal) { if (child?.pid) { try { process.kill(-child.pid, signal); } catch (error) { if (error.code !== 'ESRCH') throw error; } } }
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => { interrupted = true; signalGroup('SIGTERM'); });
async function run(command, args, cwd, env = process.env) {
  if (interrupted) throw new Error('Integration interrupted');
  await new Promise((accept, reject) => {
    child = spawn(command, args, { cwd, env, stdio: 'inherit', detached: true });
    const timer = setTimeout(() => signalGroup('SIGKILL'), 300_000);
    child.once('error', error => { clearTimeout(timer); child = undefined; reject(error); });
    child.once('exit', (code, signal) => {
      clearTimeout(timer);
      // Reap only descendants of our own process group, including an orphaned test API.
      signalGroup('SIGKILL'); child = undefined;
      if (code === 0 && !interrupted) accept(); else reject(new Error(`${command} failed: code=${code} signal=${signal}`));
    });
  });
}
try {
  await run('go', ['run', './tools/testenv', 'check', 'http'], resolve(root, 'backend'));
  for (const port of [18888, 5173, 5174, 5175, 4173, 4174, 4175]) {
    await new Promise((accept, reject) => {
      const server = createServer(); server.once('error', reject);
      server.listen(port, '127.0.0.1', () => server.close(accept));
    });
  }
  const runs = resolve(root, '.tools/http-runs'); await mkdir(runs, { recursive: true, mode: 0o700 });
  const dir = await mkdtemp(resolve(runs, 'run-'));
  console.log(`HTTP evidence: ${dir}`);
  const config = (await readFile(resolve(root, 'backend/api/etc/platform.yaml'), 'utf8'))
    .replace('Port: 8888', 'Port: 18888').replace('Environment: local', 'Environment: frontend_test')
    .replaceAll('Enabled: true', 'Enabled: false');
  await writeFile(resolve(dir, 'platform.yaml'), config, { mode: 0o600 });
  await run('go', ['build', '-o', resolve(dir, 'api'), './api'], resolve(root, 'backend'));
  await run('go', ['build', '-o', resolve(dir, 'testenv'), './tools/testenv'], resolve(root, 'backend'));
  await run('pnpm', ['build'], resolve(root, 'frontend'));
  await run('pnpm', ['exec', 'playwright', 'test', '--config', 'playwright.integration.config.ts'], resolve(root, 'frontend'), {
    ...process.env, DEXWALLET_MYSQL_DSN: process.env.DEXWALLET_TEST_HTTP_MYSQL_DSN,
    DEXWALLET_INTEGRATION_DIR: dir, DEXWALLET_API_PROXY_TARGET: 'http://127.0.0.1:18888',
    // Application and browsers never inherit the test preparation account.
    DEXWALLET_TEST_MYSQL_DSN: '',
  });
} catch (error) { console.error(error.message); process.exitCode = 1; }
