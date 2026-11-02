// Operates the unmodified MetaMask UI; reads only its unlock password, never the user's key.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const mode = process.argv[2] ?? '--connect';
const renew = process.argv.includes('--renew-permissions');
assert(['--connect', '--reject', '--pay'].includes(mode));
assert(process.argv.slice(3).every(arg => arg === '--renew-permissions'));
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const base = 'http://127.0.0.1:18793';
const manifest = JSON.parse(await readFile(`${root}/metamask-extension/manifest.json`, 'utf8'));
assert.equal(manifest.version, '13.55.0.0', 'Recheck wallet UI after upgrading');
const { metamaskPassword } = JSON.parse(await readFile(`${root}/secrets.json`, 'utf8'));
assert(metamaskPassword, 'Initialize the dedicated profile and import its test account first');
const extension = `${root}/metamask-extension`;
const context = await chromium.launchPersistentContext(`${root}/metamask-profile`, {
  channel: 'chromium', headless: true, locale: 'en-US', viewport: { width: 800, height: 600 },
  proxy: { server: 'http://127.0.0.1:7890', bypass: '127.0.0.1,localhost,base-sepolia-rpc.publicnode.com' },
  args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`],
});
context.setDefaultTimeout(10000);
const evidence = { startedAt: new Date().toISOString(), wallet: 'MetaMask', version: manifest.version, mode, renewedPermissions: renew };
const pause = () => new Promise(resolve => setTimeout(resolve, 250));
async function api(path, body) {
  const response = await fetch(base + path, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : undefined);
  const value = await response.json();
  assert(response.ok, JSON.stringify(value)); return value;
}
try {
  const config = await api('/config');
  const worker = context.serviceWorkers()[0] ?? await context.waitForEvent('serviceworker');
  const origin = `chrome-extension://${new URL(worker.url()).hostname}`;
  evidence.runtimeExtensionId = new URL(worker.url()).hostname;
  const wallet = await context.newPage();
  for (const page of context.pages()) if (page !== wallet) await page.close();
  await wallet.goto(`${origin}/home.html`);
  await wallet.locator('input[type=password]').fill(metamaskPassword);
  await wallet.getByRole('button', { name: 'Unlock', exact: true }).click();
  await wallet.getByRole('button', { name: 'Imported Account 1', exact: true }).waitFor();
  const app = await context.newPage(); await app.goto(base);
  await app.waitForFunction(() => !!window.ethereum?.isMetaMask);
  const notice = await context.newPage();
  // Headless Chromium did not automatically open the notification window. Opening the
  // original extension UI in a tab presents the same pending approvals without changing wallet code.
  await notice.goto(`${origin}/notification.html`);
  evidence.notificationUiOpenedInTab = true;
  await app.locator(renew ? '#reconnect' : '#connect').click();
  for (const started = Date.now(); Date.now() - started < 90000;) {
    const value = await app.evaluate(() => window.baseProbe.last);
    if (value?.error) throw new Error(JSON.stringify(value));
    if (value?.connected) { evidence.connection = value; break; }
    const connect = notice.getByRole('button', { name: 'Connect', exact: true });
    if (await connect.isVisible()) await connect.click();
    const body = await notice.locator('body').innerText();
    if (body.includes('Add Base Sepolia') && body.includes('base-sepolia-rpc.publicnode.com')) {
      await notice.getByRole('button', { name: 'Confirm', exact: true }).click();
    }
    await pause();
  }
  assert.equal(evidence.connection?.account.toLowerCase(), config.addresses.user.toLowerCase());
  assert.equal(Number(evidence.connection.chainId), config.chainId);
  console.log('Connected dedicated wallet on Base Sepolia');
  if (mode !== '--connect') {
    await app.locator('#sign').click();
    let clicked = false;
    for (const started = Date.now(); Date.now() - started < 90000;) {
      const value = await app.evaluate(() => window.baseProbe.last);
      if (value?.error) {
        assert(mode === '--reject' && value.code === 4001 && clicked, JSON.stringify(value));
        evidence.rejection = value; break;
      }
      if (value?.status === 'validated') { evidence.authorization = value; break; }
      if (!clicked) {
        const body = await notice.locator('body').innerText();
        if (body.includes('TransferWithAuthorization')) {
          assert(body.includes('Base Sepolia') && body.includes('10000') && body.includes('0x036Cb'), 'Unexpected wallet authorization preview');
          evidence.signaturePreview = body;
          await notice.screenshot({ path: `${root}/base-metamask-wallet-${mode.slice(2)}.png` });
          await notice.getByRole('button', { name: mode === '--reject' ? 'Cancel' : 'Confirm', exact: true }).click();
          clicked = true;
        }
      }
      await pause();
    }
    if (mode === '--reject') {
      assert(evidence.rejection);
      evidence.state = await api('/state');
      assert.equal(evidence.state.status, 'prepared'); assert(!evidence.state.txHash);
    } else {
      assert.equal(evidence.authorization?.status, 'validated', 'Wallet signature not validated');
      console.log('Real wallet authorization validated; submitting via sponsor');
      const body = { reference: evidence.authorization.reference };
      evidence.submission = await api('/submit', body);
      // Deliberately replay the same bytes; no new transaction nonce or authorization is created.
      evidence.replay = await api('/replay', body);
      evidence.repeatedSubmit = await api('/submit', body);
      assert.equal(evidence.repeatedSubmit.txHash, evidence.submission.txHash);
      assert(evidence.repeatedSubmit.reusedSubmission);
      console.log(JSON.stringify({ txHash: evidence.submission.txHash, status: evidence.repeatedSubmit.status, replay: evidence.replay.replay }));
    }
  }
} catch (error) { evidence.error = error.message; process.exitCode = 1; console.error(error.message); }
finally {
  evidence.finishedAt = new Date().toISOString();
  await writeFile(`${root}/base-metamask-wallet-${mode.slice(2)}.json`, JSON.stringify(evidence, null, 2) + '\n');
  await context.close();
}
