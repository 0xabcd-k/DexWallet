import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'node:path';
function boundaryCheck(root: string): Plugin {
  return { name: 'dexwallet-build-boundaries', apply: 'build', generateBundle(_options, bundle) {
    for (const output of Object.values(bundle)) {
      if (output.type !== 'chunk') continue;
      for (const id of Object.keys(output.modules)) {
        const path = id.replaceAll('\\', '/');
        if (path.includes('/view/dev/') || path.includes('/dev/DevPage')) this.error('Development module entered production build');
        if (root.endsWith('/user') && (path.includes('/node_modules/antd/') || path.includes('/packages/ui-desktop/'))) {
          this.error('Desktop UI entered user build');
        }
      }
    }
  } };
}
export function appConfig(root: string, port: number) {
  return defineConfig({
    root, plugins: [react(), boundaryCheck(root)], resolve: { alias: { '@': resolve(root, 'src') } },
    server: { port, strictPort: true, proxy: { '/api': {
      target: process.env.DEXWALLET_API_PROXY_TARGET ?? 'http://127.0.0.1:8888',
      rewrite: path => path.replace(/^\/api(?=\/|$)/, ''),
    } } },
    build: { manifest: true },
  });
}
