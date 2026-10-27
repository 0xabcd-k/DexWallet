import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests/integration', workers: 1, retries: 0, reporter: 'list',
  use: { browserName: 'chromium', locale: 'en-US', trace: 'retain-on-failure' },
  webServer: { command: 'pnpm --filter @dexwallet/user dev', url: 'http://127.0.0.1:5173', reuseExistingServer: false },
});
