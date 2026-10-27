import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests/e2e', fullyParallel: true, workers: 4, retries: 0, reporter: 'list',
  use: { trace: 'retain-on-failure', screenshot: 'only-on-failure', locale: 'en-US' },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
  webServer: [ ...['user', 'merchant', 'admin'].map((app, i) => ({
    command: `pnpm --filter @dexwallet/${app} dev`, url: `http://127.0.0.1:${5173 + i}`,
    reuseExistingServer: false, timeout: 60_000,
  })), ...['user', 'merchant', 'admin'].map((app, i) => ({
    command: `pnpm --filter @dexwallet/${app} preview --host 127.0.0.1 --port ${4173 + i} --strictPort`, url: `http://127.0.0.1:${4173 + i}`,
    reuseExistingServer: false, timeout: 60_000,
  })) ],
});
