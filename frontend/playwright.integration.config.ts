import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests/integration', workers: 1, retries: 0, reporter: 'list', timeout: 90_000,
  use: { browserName: 'chromium', locale: 'en-US', trace: 'retain-on-failure' },
  webServer: ['user', 'merchant', 'admin'].flatMap((app, i) => [
    { command: `pnpm --filter @dexwallet/${app} dev --host 127.0.0.1`, url: `http://127.0.0.1:${5173+i}`, reuseExistingServer: false },
    { command: `pnpm --filter @dexwallet/${app} preview --host 127.0.0.1 --port ${4173+i} --strictPort`, url: `http://127.0.0.1:${4173+i}`, reuseExistingServer: false },
  ]),
});
