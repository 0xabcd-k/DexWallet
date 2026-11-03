import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { execFileSync } from 'node:child_process';
import { Address } from '@ton/ton';
import { USER, PUBLIC_KEY } from './ton-live.mjs';
import { state, plan, save } from './ton-jetton.mjs';

const mode = process.argv[2]; assert(['claim', 'payment'].includes(mode));
const origin = process.env.PAYMENT_PROBE_PUBLIC_ORIGIN; assert(origin?.startsWith('https://'));
const before = await state(), expected = plan(mode, before);
if (mode === 'claim') assert.equal(before.accounts.user.token.balance, '0');
else assert(BigInt(before.accounts.user.token.balance) >= BigInt(expected.tokenAmount));
const proxyServer = process.env.PAYMENT_PROBE_BROWSER_PROXY;
const browser = await chromium.launch({ headless: true, ...(proxyServer ? { proxy: { server: proxyServer, bypass: '127.0.0.1,localhost' } } : {}) });
try {
  const page = await browser.newPage();
  page.on('requestfailed', req => console.error('Browser request failed:', new URL(req.url()).hostname, req.failure()?.errorText));
  await page.goto(`http://127.0.0.1:18791/?manifestOrigin=${encodeURIComponent(origin)}`);
  const link = await page.evaluate(() => window.paymentProbe.connectBridge());
  execFileSync('/home/kelizhang/Android/Sdk/platform-tools/adb', ['-s', 'emulator-5580', 'shell', 'am', 'start', '-W', '-a', 'android.intent.action.VIEW', '-d', `'${link.replaceAll("'", "'\\''")}'`], { stdio: 'pipe' });
  console.log('Waiting for original Tonkeeper connection.');
  await page.waitForFunction(() => window.paymentProbe.connector.connected, undefined, { timeout: 180000 });
  const wallet = await page.evaluate(() => window.paymentProbe.connector.wallet);
  assert.equal(wallet.account.address, USER); assert.equal(wallet.account.chain, '-3'); assert.equal(wallet.account.publicKey, PUBLIC_KEY);
  assert(wallet.device.features.some(f => typeof f === 'object' && f.name === 'SignMessage'));
  const request = { network: '-3', validUntil: Math.floor(Date.now() / 1000) + 300,
    messages: [{ address: Address.parse(expected.target).toString({ testOnly: true }), amount: expected.amount, payload: expected.payload }] };
  console.log(JSON.stringify({ mode, target: expected.target, tokenAmount: expected.tokenAmount, attachedTon: expected.amount, validUntil: request.validUntil }));
  const result = await page.evaluate(request => window.paymentProbe.connector.signMessage(request, { signal: AbortSignal.timeout(180000) }), request);
  const filename = `${mode}-capture-${Date.now()}.json`;
  await save(filename, { observedAt: new Date().toISOString(), mode, before, expected, request, account: wallet.account, device: wallet.device, result }, true);
  console.log(JSON.stringify({ mode, privateCapture: filename }));
} finally { await browser.close(); }
