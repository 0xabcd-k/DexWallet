// Uses an initialized official TronLink profile. Never loads a user private key.
// --inspect is read-only; --sign checks a rejection then returns a signature without broadcasting.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { post } from './rpc.mjs';
import { ENDPOINT, checkTransfer, verifyWalletSignature } from './tron-live-core.mjs';
import { USDT, AMOUNT_UNITS, checkTransfer as checkUSDTTransfer, verifySignature as verifyUSDTSignature } from './tron-usdt-core.mjs';
import { checkCall, verifySignature as verifyEnergySignature } from './tron-energy-core.mjs';
import { FIXTURE_SYMBOL } from './tron-resource-core.mjs';

const mode = process.argv[2] ?? '--inspect';
const asset = process.argv[3] ?? 'trx';
assert(process.argv.length <= 4 && ['--inspect', '--sign', '--sign-metadata'].includes(mode) && ['trx', 'usdt', 'energy', 'usdt-resource'].includes(asset));
const tokenPayment = ['usdt', 'usdt-resource'].includes(asset);
assert(mode !== '--sign-metadata' || tokenPayment, 'Metadata inspection requires a known Nile token probe');
const probe = asset === 'usdt-resource' ? { prefix: 'tron-resource', amount: `0.01 ${FIXTURE_SYMBOL}`,
  check: tx => checkUSDTTransfer(tx, state.addresses, state.token),
  verify: (order, tx, now) => verifyUSDTSignature(order, tx, now, state.token) }
  : asset === 'energy' ? { prefix: 'tron-energy', amount: 'Energy resource probe; no token transfer',
  check: tx => checkCall(tx, state.addresses, state.contractAddress), verify: (_order, tx, now) => verifyEnergySignature(state, tx, now) }
  : asset === 'usdt' ? { prefix: 'tron-usdt', amount: '0.01 USDT', check: checkUSDTTransfer, verify: verifyUSDTSignature }
  : { prefix: 'tron-live', amount: '0.001 TRX', check: checkTransfer, verify: verifyWalletSignature };
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const extension = `${root}/tronlink-extension`;
const manifest = JSON.parse(await readFile(`${extension}/manifest.json`, 'utf8'));
assert.equal(manifest.version, '4.11.2', 'Recheck original wallet UI after upgrades');
const state = JSON.parse(await readFile(`${root}/${probe.prefix}-state.json`, 'utf8'));
const evidence = { observedAt: new Date().toISOString(), mode, asset, walletVersion: manifest.version, addresses: state.addresses };
const prefix = `${root}/tronlink-${asset}-${mode.slice(2)}-${new Date().toISOString().replaceAll(':', '-')}`;
const query = (method, body) => post(`${ENDPOINT}/wallet/${method}`, body, { direct: true });
if (mode !== '--inspect') {
  assert(state.order && !state.payment && !state.call, 'Inspect the existing payment before requesting another signature');
  probe.check(state.order.unsigned, state.addresses);
  assert(Date.now() < state.order.unsigned.raw_data.expiration, 'Prepared transaction has expired');
}
const server = createServer((req, res) => {
  if (req.headers.host !== '127.0.0.1:18795' || req.method !== 'GET' || req.url !== '/') {
    res.writeHead(404); res.end(); return;
  }
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
  const intent = asset === 'usdt-resource' ? `${probe.amount} to the platform test address. Self-issued same-code test token, NOT Tether USDT.`
    : asset === 'energy' ? 'Call the dedicated Energy test contract; no token transfer.' : `${probe.amount} to the platform test address.`;
  res.end(`<!doctype html><title>TRON Nile signing probe</title><h1>Dedicated Nile test</h1><p>${intent} Wallet signs only; backend validates and broadcasts separately.</p>`);
});
await new Promise((resolve, reject) => { server.once('error', reject); server.listen(18795, '127.0.0.1', resolve); });
let context;
try {
  context = await chromium.launchPersistentContext(`${root}/tronlink-profile`, {
    channel: 'chromium', headless: true, locale: 'en-US', viewport: { width: 900, height: 800 },
    // Nile RPC is reachable directly; the wallet's cloud API needs the local proxy.
    proxy: { server: 'http://127.0.0.1:7890', bypass: '127.0.0.1,localhost,nile.trongrid.io,api.nileex.io' },
    args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`],
  });
  context.setDefaultTimeout(15000);
  evidence.networkFailures = [];
  context.on('requestfailed', request => {
    const url = new URL(request.url());
    evidence.networkFailures.push({ endpoint: url.origin + url.pathname, error: request.failure()?.errorText });
  });
  context.on('response', async response => {
    if (response.status() >= 400) {
      const url = new URL(response.url());
      evidence.networkFailures.push({ endpoint: url.origin + url.pathname, status: response.status() });
    }
    // TronLink can return HTTP 200 with an application authentication error.
    // Save diagnostics only, never its request authentication headers.
    if (new URL(response.url()).hostname === 'niletest.tronlink.org') {
      try {
        const body = await response.json();
        if (body.code === 4000) evidence.networkFailures.push({ endpoint: new URL(response.url()).pathname,
          status: response.status(), code: body.code, message: body.message });
      } catch { /* Response may be unavailable while the dedicated browser closes. */ }
    }
  });
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
  if (mode !== '--inspect') {
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
      if (asset === 'energy') await notice.getByText('Metadata', { exact: true }).waitFor();
      if (tokenPayment) {
        try { await notice.waitForFunction(amount => document.body.innerText.replace(/\s+/g, ' ').includes(amount), probe.amount, { timeout: 20000 }); }
        catch { evidence.amountPreviewTimedOut = true; }
      }
      const preview = (await notice.locator('body').innerText()).replace(/\s+/g, ' ');
      evidence.preview = preview;
      await notice.screenshot({ path: `${prefix}-${decision.toLowerCase()}.png` });
      if (asset === 'energy' || (tokenPayment && !preview.includes(probe.amount))) {
        await notice.getByText('Metadata', { exact: true }).click();
        evidence.metadataPreview = await notice.locator('body').innerText();
        await notice.screenshot({ path: `${prefix}-metadata.png` });
      }
      const expectedPreview = asset === 'energy' ? ['TRON Nile Testnet', state.contractAddress, 'No expected asset change']
        : ['TRON Nile Testnet', state.addresses.user, state.addresses.sponsor];
      for (const expected of expectedPreview) assert(preview.includes(expected), `Missing wallet preview: ${expected}`);
      if (asset === 'energy') {
        assert(evidence.metadataPreview.includes('probe()'));
        assert(evidence.metadataPreview.includes(state.order.unsigned.raw_data_hex), 'Wallet metadata differs from prepared bytes');
      } else if (mode === '--sign-metadata' && !preview.includes(probe.amount)) {
        assert(preview.includes('Token precision error') && preview.includes(asset === 'usdt-resource' ? state.token : USDT));
        const metadata = evidence.metadataPreview;
        assert(metadata.includes('transfer(address,uint256)') && metadata.includes(state.addresses.sponsor));
        assert(metadata.includes(`value:\n${AMOUNT_UNITS}`));
        assert(metadata.includes(state.order.unsigned.raw_data_hex), 'Wallet metadata differs from exact prepared bytes');
        evidence.reviewedUsingRawMetadata = true;
        evidence.humanAmountPreviewCorrect = false;
      } else assert(preview.includes(probe.amount), `Missing wallet preview: ${probe.amount}`);
      await notice.getByText(decision, { exact: true }).click();
      await app.waitForFunction(() => window.signResult !== null);
      const result = await app.evaluate(() => window.signResult);
      if (decision === 'Reject') {
        assert.deepEqual(result, { ok: false, error: 'Confirmation declined by user' });
        evidence.rejection = result;
      } else {
        assert(result.ok, 'Wallet did not sign');
        evidence.signature = probe.verify(state.order, result.value, Date.now());
        await writeFile(`${root}/${probe.prefix}-wallet-signed.json`, JSON.stringify(result.value, null, 2) + '\n', { mode: 0o600 });
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
