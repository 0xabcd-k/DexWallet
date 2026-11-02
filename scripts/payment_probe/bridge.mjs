import { chromium } from 'playwright';
import { execFileSync } from 'node:child_process';

// Uses an isolated Chromium session; approve the connection on emulator-5580.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();
try {
  const manifestOrigin = process.env.PAYMENT_PROBE_PUBLIC_ORIGIN ?? 'http://10.0.2.2:18791';
  await page.goto(`http://127.0.0.1:18791/?manifestOrigin=${encodeURIComponent(manifestOrigin)}`);
  const link = await page.evaluate(() => window.paymentProbe.connectBridge());
  // Pass one shell-quoted argument to Android's shell. No key or seed is in the link.
  const quoted = `'${link.replaceAll("'", "'\\''")}'`;
  execFileSync('/home/kelizhang/Android/Sdk/platform-tools/adb', ['-s', 'emulator-5580', 'shell', 'am', 'start', '-W', '-a', 'android.intent.action.VIEW', '-d', quoted], { stdio: 'pipe' });
  console.log('Connection requested; waiting up to 180 seconds for wallet approval.');
  await page.waitForFunction(() => window.paymentProbe.connector.connected || window.paymentProbe.lastEvent?.type === 'connection-error', undefined, { timeout: 180000 });
  const connectionError = await page.evaluate(() => window.paymentProbe.lastEvent?.type === 'connection-error' ? window.paymentProbe.lastEvent.value : null);
  if (connectionError) throw new Error(`${connectionError.name}: ${connectionError.message}`);
  await page.getByRole('button', { name: 'Check sign-only capability' }).click();
  await page.waitForFunction(() => ['sign-error', 'sign-result'].includes(window.paymentProbe.lastEvent?.type), undefined, { timeout: 65000 });
  console.log(await page.locator('#result').innerText());
} finally { await browser.close(); }
