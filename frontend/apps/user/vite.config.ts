import { appConfig } from '../../vite.shared.ts';
import { defineConfig, loadEnv } from 'vite';
import { telegramDev } from '../../telegram.dev.ts';

export default defineConfig(({ command, mode }) => {
  const native = mode === 'native';
  const env = loadEnv(mode, import.meta.dirname, 'DEXWALLET_APP_');
  if (native) {
    const api = new URL(env.DEXWALLET_APP_API_URL ?? '');
    const localHTTP = env.DEXWALLET_APP_DEBUG_HTTP === '1' && api.protocol === 'http:' && ['localhost', '127.0.0.1', '10.0.2.2'].includes(api.hostname);
    if ((api.protocol !== 'https:' && !localHTTP) || api.username || api.password || api.search || api.hash) throw new Error('App API requires HTTPS; debug HTTP is limited to the local emulator');
    if (env.DEXWALLET_APP_TON_MANIFEST_URL) {
      const manifest = new URL(env.DEXWALLET_APP_TON_MANIFEST_URL);
      if (manifest.protocol !== 'https:' || manifest.username || manifest.password) throw new Error('TON manifest requires HTTPS');
    }
  }
  const config = appConfig(import.meta.dirname, 5173);
  const mockTelegram = command === 'serve' && mode === 'telegram';
  return { ...config, ...(native ? { build: { ...config.build, outDir: 'dist-app' } } : {}), define: { 'import.meta.env.NATIVE_APP': JSON.stringify(native), 'import.meta.env.APP_API_URL': JSON.stringify(env.DEXWALLET_APP_API_URL ?? ''), 'import.meta.env.TON_MANIFEST_URL': JSON.stringify(env.DEXWALLET_APP_TON_MANIFEST_URL ?? ''), 'import.meta.env.DEV_TELEGRAM': JSON.stringify(mockTelegram) },
    plugins: [...config.plugins!, ...(native ? [{ name: 'native-entry', transformIndexHtml: { order: 'pre' as const, handler: (html: string) => html.replace('src="/src/main.tsx"', 'src="/src/native-main.tsx"') } }] : []), ...(mockTelegram ? [telegramDev()] : [])] };
});
