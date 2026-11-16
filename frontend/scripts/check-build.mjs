import { readFile, readdir } from 'node:fs/promises';
import { resolve } from 'node:path';
for (const app of ['user', 'merchant', 'admin']) {
  const folder = resolve('apps', app, 'dist');
  const manifest = JSON.parse(await readFile(resolve(folder, '.vite/manifest.json'), 'utf8'));
  if (!manifest['index.html']?.isEntry) throw new Error(`${app}: missing entry`);
  const assets = await readdir(resolve(folder, 'assets'));
  const content = (await Promise.all(assets.filter(f => /\.(js|css)$/.test(f)).map(f => readFile(resolve(folder, 'assets', f), 'utf8')))).join('\n');
  if (Object.keys(manifest).some(key => key.includes('/view/dev/') || key.includes('/dev/DevPage'))) throw new Error(`${app}: development route shipped`);
  if (app === 'user' && content.includes('.desktop-shell')) throw new Error('User build includes desktop UI');
  if (app === 'user' && Object.keys(manifest).some(key => key.includes('/native/') || key.includes('/wallet-connections/') || key.includes('/local-wallets/') || key.includes('/chain-assets/') || key.includes('/chain-transfer/'))) throw new Error('Native wallet modules entered Mini App build');
  console.log(`PASS ${app}: production entry and development/desktop isolation`);
}
