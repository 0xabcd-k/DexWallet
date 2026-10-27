import { spawnSync } from 'node:child_process';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { createServer } from 'node:net';
const root = fileURLToPath(new URL('../../', import.meta.url));
if (!process.env.DEXWALLET_TEST_MYSQL_DSN) throw new Error('Set DEXWALLET_TEST_MYSQL_DSN to a dedicated test database; no database is created or migrated by this runner.');
// Fail on an occupied port before tests can stop or send requests to another process.
await new Promise((accept, reject) => { const server = createServer(); server.once('error', reject); server.listen(18888, '127.0.0.1', () => server.close(accept)); });
const dir = resolve(root, '.tools/frontend-integration');
await mkdir(dir, { recursive: true });
const config = (await readFile(resolve(root, 'backend/api/etc/platform.yaml'), 'utf8'))
  .replace('Port: 8888', 'Port: 18888').replace('Environment: local', 'Environment: frontend_test')
  .replaceAll('Enabled: true', 'Enabled: false');
await writeFile(resolve(dir, 'platform.yaml'), config);
const build = spawnSync('go', ['build', '-o', resolve(dir, 'api'), './api'], { cwd: resolve(root, 'backend'), stdio: 'inherit' });
if (build.status !== 0) process.exit(build.status ?? 1);
const test = spawnSync('pnpm', ['exec', 'playwright', 'test', '--config', 'playwright.integration.config.ts'], {
  cwd: resolve(root, 'frontend'), stdio: 'inherit', env: { ...process.env,
    DEXWALLET_MYSQL_DSN: process.env.DEXWALLET_TEST_MYSQL_DSN,
    DEXWALLET_INTEGRATION_DIR: dir, DEXWALLET_API_PROXY_TARGET: 'http://127.0.0.1:18888',
  },
});
process.exit(test.status ?? 1);
