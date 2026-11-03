// Uses an initialized official TronLink profile. Never loads a user private key.
// --inspect is read-only; --sign checks a rejection then returns a signature without broadcasting.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { post } from './rpc.mjs';
import { ENDPOINT, checkTransfer, verifyWalletSignature } from './tron-live-core.mjs';

const mode = process.argv[2] ?? '--inspect';
assert(process.argv.length <= 3 && ['--inspect', '--sign'].includes(mode));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const extension = `${root}/tronlink-extension`;
const manifest = JSON.parse(await readFile(`${extension}/manifest.json`, 'utf8'));
assert.equal(manifest.version, '4.11.2', 'Recheck original wallet UI after upgrades');
const state = JSON.parse(await readFile(`${root}/tron-live-state.json`, 'utf8'));
const evidence = { observedAt: new Date().toISOString(), mode, walletVersion: manifest.version, addresses: state.addresses };
const prefix = `${root}/tronlink-${mode.slice(2)}-${new Date().toISOString().replaceAll(':', '-')}`;
const query = (method, body) => post(`${ENDPOINT}/wallet/${method}`, body, { direct: true });
if (mode === '--sign') {
  assert(state.order && !state.payment, 'Inspect the existing payment before requesting another signature');
  checkTransfer(state.order.unsigned, state.addresses);
  assert(Date.now() < state.order.unsigned.raw_data.expiration, 'Prepared transaction has expired');
}
const server = createServer((req, res) => {
  if (req.headers.host !== '127.0.0.1:18795' || req.method !== 'GET' || req.url !== '/') {
    res.writeHead(404); res.end(); return;
  }
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
  res.end('<!doctype html><title>TRON Nile signing probe</title><h1>Dedicated Nile test</h1><p>0.001 test TRX to the platform test address. Wallet signs only; backend validates and broadcasts separately.</p>');
});
await new Promise((resolve, reject) => { server.once('error', reject); server.listen(18795, '127.0.0.1', resolve); });
let context;
try {
  context = await chromium.launchPersistentContext(`${root}/tronlink-profile`, {
    channel: 'chromium', headless: true, locale: 'en-US', viewport: { width: 900, height: 800 },
    proxy: { server: 'http://127.0.0.1:7890', bypass: '127.0.0.1,localhost,nile.trongrid.io,api.nileex.io' },
    args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`],
  });
  context.setDefaultTimeout(15000);
  const worker = context.serviceWorkers()[0] ?? await context.waitForEvent('serviceworker');
  const origin = `chrome-extension://${new URL(worker.url()).hostname}`;
  const home = await context.newPage();
  for (const page of context.pages()) if (page !== home) await page.close();
  await home.goto(`${origin}/popup/popup.html`);
  try {
    const secrets = JSON.parse(await readFile(`${root}/tronlink-secrets.json`, 'utf8'));
    await home.locator('input[type=password]').pressSequentially(secrets.password);
    await home.getByText('Unlock', { exact: true }).click();
    await home.waitForURL(url => url.hash === '#/home');
  } catch { throw new Error('TronLink unlock failed; inspect the dedicated profile locally'); }
  const app = await context.newPage(); await app.goto('http://127.0.0.1:18795/');
  await app.waitForFunction(() => !!window.tron?.tronWeb);
  // Initial connection / network switch must have been approved in the original wallet UI.
  evidence.accounts = await app.evaluate(() => window.tron.request({ method: 'eth_requestAccounts' }));
  assert.deepEqual(evidence.accounts, [state.addresses.user]);
  evidence.network = await app.evaluate(() => ({ address: window.tron.tronWeb.defaultAddress.base58,
    fullNode: window.tron.tronWeb.fullNode.host, solidityNode: window.tron.tronWeb.solidityNode.host }));
  assert.equal(evidence.network.address, state.addresses.user);
  assert.equal(evidence.network.fullNode, ENDPOINT); assert.equal(evidence.network.solidityNode, ENDPOINT);
  if (mode === '--sign') {
    evidence.txID = state.order.unsigned.txID;
    for (const decision of ['Reject', 'Sign']) {
      assert(!(await query('gettransactionbyid', { value: evidence.txID })).txID, 'Transaction was already broadcast');
      await app.evaluate(tx => {
        window.signResult = null;
        window.tron.tronWeb.trx.sign(tx).then(value => { window.signResult = { ok: true, value }; },
          error => { window.signResult = { ok: false, error: typeof error === 'string' ? error : { code: error.code, message: error.message } }; });
      }, state.order.unsigned);
      let notice;
      for (const start = Date.now(); Date.now() - start < 15000;) {
        notice = context.pages().find(page => !page.isClosed() && page.url().includes('secondary_popup'));
        if (notice) break;
        await new Promise(resolve => setTimeout(resolve, 100));
      }
      assert(notice, 'No original wallet signature popup');
      await notice.getByText('Sign', { exact: true }).waitFor();
      const preview = await notice.locator('body').innerText();
      for (const expected of ['TRON Nile Testnet', '0.001 TRX', state.addresses.user, state.addresses.sponsor]) assert(preview.includes(expected), `Missing wallet preview: ${expected}`);
      evidence.preview = preview;
      await notice.screenshot({ path: `${prefix}-${decision.toLowerCase()}.png` });
      await notice.getByText(decision, { exact: true }).click();
      await app.waitForFunction(() => window.signResult !== null);
      const result = await app.evaluate(() => window.signResult);
      if (decision === 'Reject') {
        assert.deepEqual(result, { ok: false, error: 'Confirmation declined by user' });
        evidence.rejection = result;
      } else {
        assert(result.ok, 'Wallet did not sign');
        evidence.signature = verifyWalletSignature(state.order, result.value, Date.now());
        await writeFile(`${root}/tron-live-wallet-signed.json`, JSON.stringify(result.value, null, 2) + '\n', { mode: 0o600 });
      }
      assert(!(await query('gettransactionbyid', { value: evidence.txID })).txID, 'Wallet unexpectedly broadcast');
    }
    evidence.signedNotBroadcast = true;
  }
} catch (error) { evidence.error = error.message; process.exitCode = 1; }
finally {
  await writeFile(`${prefix}.json`, JSON.stringify(evidence, null, 2) + '\n', { mode: 0o600 });
  await context?.close(); await new Promise(resolve => server.close(resolve));
  console.log(JSON.stringify({ mode, network: evidence.network, error: evidence.error, evidenceFile: `${prefix}.json` }));
}
