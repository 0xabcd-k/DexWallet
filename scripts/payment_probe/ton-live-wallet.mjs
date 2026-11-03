import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { execFileSync } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { Address } from '@ton/ton';
import { USER, PLATFORM, PUBLIC_KEY, PAYMENT, directory, snapshot } from './ton-live.mjs';

// Original Android wallet handles user signing. Initialization uses sendTransaction
// explicitly; payment uses signMessage and must be relayed separately by the backend.
const mode = process.argv[2];
assert(['initialize', 'payment'].includes(mode), 'Usage: ton-live-wallet.mjs initialize|payment');
const manifestOrigin = process.env.PAYMENT_PROBE_PUBLIC_ORIGIN;
assert(manifestOrigin?.startsWith('https://'), 'Provide reachable HTTPS manifest origin');
await mkdir(directory, { recursive: true, mode: 0o700 });
const before = await snapshot();
assert(BigInt(before.user.balance) >= (mode === 'initialize' ? 10_000_000n : PAYMENT));
if (mode === 'initialize') assert.equal(before.user.seqno, 0);
else assert(before.user.seqno > 0);
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage();
  await page.goto(`http://127.0.0.1:18791/?manifestOrigin=${encodeURIComponent(manifestOrigin)}`);
  const link = await page.evaluate(() => window.paymentProbe.connectBridge());
  const quoted = `'${link.replaceAll("'", "'\\''")}'`;
  execFileSync('/home/kelizhang/Android/Sdk/platform-tools/adb', ['-s', 'emulator-5580', 'shell', 'am', 'start', '-W', '-a', 'android.intent.action.VIEW', '-d', quoted], { stdio: 'pipe' });
  console.log('Waiting for original wallet connection.');
  await page.waitForFunction(() => window.paymentProbe.connector.connected, undefined, { timeout: 180000 });
  const wallet = await page.evaluate(() => window.paymentProbe.connector.wallet);
  assert.equal(wallet.account.chain, '-3');
  assert.equal(wallet.account.address, USER);
  assert.equal(wallet.account.publicKey, PUBLIC_KEY);
  if (mode === 'payment') assert(wallet.device.features.some(f => typeof f === 'object' && f.name === 'SignMessage'));
  const request = { network: '-3', validUntil: Math.floor(Date.now() / 1000) + 300,
    messages: [{ address: Address.parse(mode === 'initialize' ? USER : PLATFORM).toString({ testOnly: true, bounceable: false }),
      amount: (mode === 'initialize' ? 1_000_000n : PAYMENT).toString() }] };
  console.log(JSON.stringify({ mode, request }));
  const result = await page.evaluate(async ({ mode, request }) => {
    const c = window.paymentProbe.connector;
    return mode === 'initialize'
      ? c.sendTransaction(request, { signal: AbortSignal.timeout(180000) })
      : c.signMessage(request, { signal: AbortSignal.timeout(180000) });
  }, { mode, request });
  const capture = { observedAt: new Date().toISOString(), mode, before, request, account: wallet.account, device: wallet.device, result };
  const filename = `${mode}-capture-${Date.now()}.json`;
  await writeFile(new URL(filename, directory), JSON.stringify(capture, null, 2), { mode: 0o600, flag: 'wx' });
  console.log(JSON.stringify({ mode, privateCapture: filename, returnedBoc: typeof (result.internalBoc ?? result.boc) === 'string' }));
} finally { await browser.close(); }
