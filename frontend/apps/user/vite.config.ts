import { appConfig } from '../../vite.shared.ts';
import { defineConfig } from 'vite';
import { telegramDev } from '../../telegram.dev.ts';

export default defineConfig(({ command, mode }) => {
  const config = appConfig(import.meta.dirname, 5173);
  const mockTelegram = command === 'serve' && mode === 'telegram';
  return { ...config, define: { 'import.meta.env.DEV_TELEGRAM': JSON.stringify(mockTelegram) },
    plugins: [...config.plugins!, ...(mockTelegram ? [telegramDev()] : [])] };
});
