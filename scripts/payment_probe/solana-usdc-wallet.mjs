// Drives the unmodified Solflare extension UI; never imports or signs with a user private key.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const mode = process.argv[2] ?? '--connect';
assert(process.argv.length <= 3 && ['--connect', '--reject', '--pay'].includes(mode), 'Use --connect, --reject or --pay');
const base = 'http://127.0.0.1:18792';
const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
async function json(path, body) {
  const response = await fetch(base + path, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : undefined);
  const value = await response.json();
  return { httpStatus: response.status, value };
}
const config = await json('/config');
assert.equal(config.httpStatus, 200);
assert.equal(config.value.asset, 'USDC');
const { probeName, addresses } = config.value;
assert(['solana-usdc-new-ata', 'solana-usdc-existing-ata'].includes(probeName));
const { solflarePassword } = JSON.parse(await readFile(`${output}/secrets.json`, 'utf8'));
assert(solflarePassword, 'Initialize the dedicated Solflare profile first');
const extension = `${output}/solflare-extension`;
const manifest = JSON.parse(await readFile(`${extension}/manifest.json`, 'utf8'));
assert.equal(manifest.version, '2.41.1', 'Recheck the UI flow for a different wallet version');
const context = await chromium.launchPersistentContext(`${output}/solflare-profile`, {
  channel: 'chromium', headless: true, locale: 'en-US', viewport: { width: 800, height: 600 },
  proxy: { server: 'http://127.0.0.1:7890', bypass: '127.0.0.1,localhost' },
  args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`],
});
context.setDefaultTimeout(10000);
const evidence = { startedAt: new Date().toISOString(), mode, probeName, wallet: 'Solflare', version: manifest.version };
const pause = () => new Promise(resolve => setTimeout(resolve, 200));
try {
  const worker = context.serviceWorkers()[0] ?? await context.waitForEvent('serviceworker');
  const origin = `chrome-extension://${new URL(worker.url()).hostname}`;
  evidence.restoredPageCount = context.pages().length;
  const wallet = await context.newPage();
  for (const page of context.pages()) if (page !== wallet) await page.close();
  await wallet.setViewportSize({ width: 800, height: 1000 });
  await wallet.goto(`${origin}/wallet.html`);
  await wallet.getByPlaceholder('Password', { exact: true }).fill(solflarePassword);
  await wallet.getByRole('button', { name: 'Unlock', exact: true }).click();
  await wallet.goto(`${origin}/wallet.html#/settings`);
  await wallet.getByText('Network', { exact: true }).waitFor();
  await wallet.getByText('Devnet', { exact: true }).waitFor(); // Require the previously confirmed setting.
  evidence.networkSetting = 'Devnet';
  const app = await context.newPage();
  await wallet.close();
  await app.goto(base);
  await app.waitForFunction(() => !!window.solflare);
  await app.locator('#connect').click();
  for (const started = Date.now(); Date.now() - started < 20000;) {
    const value = await app.evaluate(() => window.solanaProbe.last);
    if (value?.error) throw new Error(value.error);
    if (value?.connected) { evidence.connection = value; break; }
    const popup = context.pages().find(page => page.url().includes('/confirm_popup.html'));
    if (popup && await popup.getByRole('button', { name: 'Connect', exact: true }).isVisible()) {
      await popup.getByRole('button', { name: 'Connect', exact: true }).click();
    }
    await pause();
  }
  assert.equal(evidence.connection?.user, addresses.user, 'Wrong or unconnected test wallet');
  if (mode !== '--connect') {
    // Let extension startup requests settle before obtaining a short-lived blockhash.
    evidence.walletSettlingMs = 15000;
    await new Promise(resolve => setTimeout(resolve, evidence.walletSettlingMs));
    await app.locator('#sign').click();
    let approved = false, trusted = false, captured = false;
    for (const started = Date.now(); Date.now() - started < 45000;) {
      const value = await app.evaluate(() => window.solanaProbe.last);
      if (value?.error) {
        if (mode === '--reject') { evidence.rejection = value.error; break; }
        throw new Error(value.error);
      }
      if (value?.status === 'validated') { evidence.userSigned = value; break; }
      const popup = context.pages().find(page => page.url().includes('/confirm_popup.html'));
      if (popup && !approved) {
        if (await popup.getByText('Transaction expired', { exact: true }).isVisible()) throw new Error('Wallet request expired; no replacement signature is made automatically');
        if (mode === '--reject') {
          if (await popup.getByRole('button', { name: 'Reject', exact: true }).isVisible()) {
            await popup.getByRole('button', { name: 'Reject', exact: true }).click(); approved = true;
          }
        } else {
          const trust = popup.getByText('I trust this site', { exact: true });
          if (!trusted && await trust.isVisible()) {
            evidence.securityPreview = await popup.locator('body').innerText();
            await popup.screenshot({ path: `${output}/${probeName}-sign.png` }); captured = true;
            // Only the fixed local test site and fixed server USDC payment are authorized here.
            await trust.click(); trusted = true;
          }
          const approve = popup.getByRole('button', { name: 'Approve', exact: true });
          if (await approve.isEnabled().catch(() => false)) {
            if (!captured) {
              evidence.securityPreview = await popup.locator('body').innerText();
              await popup.screenshot({ path: `${output}/${probeName}-sign.png` });
            }
            await approve.click(); approved = true;
          }
        }
      }
      await pause();
    }
    if (mode === '--reject') {
      assert.match(evidence.rejection ?? '', /cancel|reject/i);
      const current = await json('/state');
      assert.equal(current.value.status, 'prepared');
      assert(!current.value.signature && !current.value.userSignature);
      evidence.serverAfterRejection = current.value;
    } else {
      assert.equal(evidence.userSigned?.status, 'validated', 'Wallet did not return a validated signature');
      assert.equal(evidence.userSigned.asset, 'USDC');
      assert.equal(evidence.userSigned.amountAtomic, '10000');
      await app.locator('#submit').click();
      await app.waitForFunction(() => window.solanaProbe.last?.signature || window.solanaProbe.last?.error, {}, { timeout: 60000 });
      const submitted = await app.evaluate(() => window.solanaProbe.last);
      if (submitted.error) throw new Error(submitted.error);
      evidence.submission = { signature: submitted.signature, submittedAt: submitted.submittedAt };
      // Immediate replay attempts retain their actual RPC result; they never change the message.
      const replay = await json('/replay', { reference: submitted.reference });
      evidence.immediateReplay = { httpStatus: replay.httpStatus, signature: replay.value.replaySignature, error: replay.value.error };
      const repeated = await json('/submit', { reference: submitted.reference });
      assert.equal(repeated.value.signature, submitted.signature);
      assert.equal(repeated.value.reusedSubmission, true);
      evidence.repeatedSubmit = { httpStatus: repeated.httpStatus, reusedSubmission: repeated.value.reusedSubmission, signature: repeated.value.signature };
    }
  }
  evidence.completedAt = new Date().toISOString();
  console.log(JSON.stringify({ mode, probeName, connected: true, rejection: evidence.rejection, submission: evidence.submission, immediateReplay: evidence.immediateReplay }, null, 2));
} catch (error) {
  evidence.error = error.message;
  throw error;
} finally {
  await writeFile(`${output}/${probeName}-wallet-${mode.slice(2)}.json`, JSON.stringify(evidence, null, 2));
  await context.close();
}
