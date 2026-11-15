import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const mobile = fileURLToPath(new URL('..', import.meta.url));
const release = process.argv.includes('--release');
if (process.argv.slice(2).some(arg => arg !== '--release')) throw new Error('Usage: build.mjs [--release]');
if (release && process.env.DEXWALLET_APP_DEBUG_HTTP === '1') throw new Error('Release builds cannot use debug HTTP');
function run(command, args, cwd = mobile) {
  const result = spawnSync(command, args, { cwd, stdio: 'inherit', env: process.env });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}
run('pnpm', ['--filter', '@dexwallet/user', 'build:app']);
const manifest = JSON.parse(readFileSync(`${mobile}/../user/dist-app/.vite/manifest.json`, 'utf8'));
if (!manifest['index.html']?.dynamicImports?.includes('src/app/native/storage.ts')) {
  throw new Error('Native entry must initialize persistent storage before loading the App');
}
run('pnpm', ['exec', 'cap', 'sync', 'android']);
run('./gradlew', [release ? 'assembleRelease' : 'assembleDebug', '--console=plain'], `${mobile}/android`);
