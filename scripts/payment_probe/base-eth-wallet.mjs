// Isolated MetaMask ERC-7715 probe. Never signs with a local user key or sends a payment.
// --grant-probe may upgrade the dedicated test account through MetaMask's original UI.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { rpc } from './rpc.mjs';

const mode = process.argv[2] ?? '--inspect';
const walletVersion = process.argv[3] ?? '13.55.0.0';
assert(process.argv.length <= 4 && ['--inspect', '--reject', '--grant-probe'].includes(mode));
assert(['13.55.0.0', '13.53.0.0'].includes(walletVersion));
const comparison = walletVersion === '13.53.0.0'; // Explicit isolated comparison, never an automatic downgrade.
const root = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const chainId = '0x14a34';
const endpoint = 'https://base-sepolia-rpc.publicnode.com';
const base = 'http://127.0.0.1:18794';
const user = '0xecd14f7586f5053cecae148ae26309c2277d86a8';
const sponsor = '0xa59c992aa84657df4ebff6f04e11a1632f6db1fa';
const allowanceAmount = '0xe8d4a51000'; // Exactly 0.000001 test ETH, no periodic renewal.
const run = `${walletVersion}-${mode.slice(2)}-${new Date().toISOString().replaceAll(':', '-')}`;
const prefix = `${root}/base-eth-wallet-${run}`;
const evidence = { startedAt: new Date().toISOString(), mode, user, sponsor, chainId };
const extension = `${root}/${comparison ? 'metamask-13.53-extension' : 'metamask-extension'}`;
const manifest = JSON.parse(await readFile(`${extension}/manifest.json`, 'utf8'));
assert.equal(manifest.version, walletVersion, 'Review wallet UI and capabilities after an upgrade');
evidence.walletVersion = manifest.version;
const secrets = JSON.parse(await readFile(`${root}/${comparison ? 'metamask-13.53-secrets.json' : 'secrets.json'}`, 'utf8'));
const metamaskPassword = comparison ? secrets.password : secrets.metamaskPassword;
assert(metamaskPassword, 'Initialize the dedicated wallet profile first');
const server = createServer((req, res) => {
  if (req.headers.host !== '127.0.0.1:18794' || req.method !== 'GET' || req.url !== '/') {
    res.writeHead(404); res.end(); return;
  }
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
  res.end('<!doctype html><title>Base ETH permission probe</title><h1>Dedicated Base Sepolia test</h1><p>0.000001 ETH maximum; fixed test payee; one hour expiry. This probe does not submit payments.</p>');
});
await new Promise((resolve, reject) => { server.once('error', reject); server.listen(18794, '127.0.0.1', resolve); });
let context;
const pause = () => new Promise(resolve => setTimeout(resolve, 250));
const query = (method, params = []) => rpc(endpoint, method, params, { direct: true });
async function chainState() {
  assert.equal(await query('eth_chainId'), chainId);
  const block = await query('eth_blockNumber');
  const [balance, code, nonce, sponsorBalance, sponsorNonce] = await Promise.all([
    query('eth_getBalance', [user, block]), query('eth_getCode', [user, block]),
    query('eth_getTransactionCount', [user, block]), query('eth_getBalance', [sponsor, block]),
    query('eth_getTransactionCount', [sponsor, block]),
  ]);
  return { block, balance, code, nonce, sponsorBalance, sponsorNonce };
}
try {
  evidence.before = await chainState();
  context = await chromium.launchPersistentContext(`${root}/${comparison ? 'metamask-13.53-profile' : 'metamask-profile'}`, {
    channel: 'chromium', headless: true, locale: 'en-US', viewport: { width: 800, height: 600 },
    proxy: { server: 'http://127.0.0.1:7890', bypass: '127.0.0.1,localhost,base-sepolia-rpc.publicnode.com' },
    args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`],
  });
  context.setDefaultTimeout(10000);
  const worker = context.serviceWorkers()[0] ?? await context.waitForEvent('serviceworker');
  const origin = `chrome-extension://${new URL(worker.url()).hostname}`;
  evidence.runtimeExtensionId = new URL(worker.url()).hostname;
  const home = await context.newPage();
  for (const page of context.pages()) if (page !== home) await page.close();
  await home.goto(`${origin}/home.html`);
  await home.locator('input[type=password]').fill(metamaskPassword);
  await home.getByRole('button', { name: 'Unlock', exact: true }).click();
  await home.getByRole('button', { name: 'Imported Account 1', exact: true }).waitFor();
  const app = await context.newPage(); await app.goto(base);
  await app.waitForFunction(() => !!window.ethereum?.isMetaMask);
  const notice = await context.newPage(); await notice.goto(`${origin}/notification.html`);
  evidence.notificationUiOpenedInTab = true;
  await app.evaluate(() => {
    window.connection = null;
    ethereum.request({ method: 'eth_requestAccounts', params: [] }).then(
      value => { window.connection = { value }; },
      error => { window.connection = { code: error.code, message: error.message }; },
    );
  });
  for (const start = Date.now(); Date.now() - start < 60000;) {
    const result = await app.evaluate(() => window.connection);
    if (result) { assert(result.value, JSON.stringify(result)); evidence.accounts = result.value; break; }
    const button = notice.getByRole('button', { name: 'Connect', exact: true });
    if (await button.isVisible()) await button.click();
    await pause();
  }
  assert.equal(evidence.accounts?.[0]?.toLowerCase(), user);
  assert.equal(await app.evaluate(() => ethereum.request({ method: 'eth_chainId', params: [] })), chainId);
  evidence.capabilities = await app.evaluate(() => ethereum.request({ method: 'wallet_getSupportedExecutionPermissions', params: [] }));
  const capability = evidence.capabilities['native-token-allowance'];
  assert(capability?.chainIds.includes(chainId));
  for (const rule of ['expiry', 'payee', 'redeemer']) assert(capability.ruleTypes.includes(rule));
  const priorGrants = await app.evaluate(() => ethereum.request({ method: 'wallet_getGrantedExecutionPermissions', params: [] }));
  evidence.priorGrantCount = priorGrants.length;
  if (mode !== '--inspect') {
    assert.equal(priorGrants.length, 0, 'Review existing permissions before requesting another allowance');
    const now = Math.floor(Date.now() / 1000);
    const request = {
      chainId, to: sponsor,
      permission: { type: 'native-token-allowance', isAdjustmentAllowed: false, data: {
        allowanceAmount, startTime: now,
        justification: 'Dedicated Base Sepolia test: 0.000001 ETH to platform test recipient; platform pays gas.',
      } },
      rules: [{ type: 'expiry', data: { timestamp: now + 3600 } },
        { type: 'redeemer', data: { addresses: [sponsor] } },
        { type: 'payee', data: { addresses: [sponsor] } }],
    };
    // Omit `from` so the original wallet UI chooses the account. Verify the returned account below.
    evidence.request = request;
    await app.evaluate(value => {
      window.permissionResult = null;
      ethereum.request({ method: 'wallet_requestExecutionPermissions', params: [value] }).then(
        result => { window.permissionResult = { ok: true, value: result }; },
        error => { window.permissionResult = { ok: false, code: error.code, message: error.message, stack: error.stack }; },
      );
    }, request);
    let result;
    let clicked = false;
    for (const start = Date.now(); Date.now() - start < 120000;) {
      result = await app.evaluate(() => window.permissionResult);
      if (result) break;
      let text = await notice.locator('body').innerText();
      const intro = notice.getByRole('button', { name: 'Got it', exact: true });
      if (await intro.isVisible() && text.includes('Token allowance request')) await intro.click();
      const grant = notice.getByRole('button', { name: 'Grant', exact: true });
      if (!clicked && await grant.isVisible()) {
        // The wallet renders the Grant button before its permission details finish loading.
        await notice.getByText('0.000001', { exact: true }).waitFor({ timeout: 30000 });
        text = await notice.locator('body').innerText();
        evidence.permissionPreview = text;
        const lower = text.toLowerCase();
        assert(text.includes('Imported Account 1') && lower.includes('0xecd14') && lower.includes('d86a8'));
        assert(text.includes('Base Sepolia') && text.includes('0.000001') && text.includes('Payees') && text.includes('Redeemers'));
        assert(lower.includes('0xa59') && lower.includes('db1fa'));
        await notice.screenshot({ path: `${prefix}-permission.png` });
        await notice.getByRole('button', { name: mode === '--reject' ? 'Cancel' : 'Grant', exact: true }).click();
        clicked = true;
        continue; // Read the next confirmation afresh; never validate it using stale preview text.
      }
      if (mode === '--grant-probe' && text.includes('Account update') && text.includes('Paid by MetaMask')) {
        assert(text.includes('Smart account') && text.includes('Imported'));
        evidence.upgradePreview = text;
        await notice.screenshot({ path: `${prefix}-upgrade.png` });
        await notice.getByRole('button', { name: 'Confirm', exact: true }).click();
        continue;
      }
      // Successful Gator authorization opens a second original-wallet signature confirmation.
      const confirm = notice.getByRole('button', { name: 'Confirm', exact: true });
      if (mode === '--grant-probe' && clicked && text.includes('Permission request') && await confirm.isVisible()) {
        assert(text.includes('Imported Account 1') && text.includes('Base Sepolia') && text.includes('0.000001'));
        assert(text.includes('Redeemer') && text.includes('Payee') && text.includes('Expiration'));
        assert(text.toLowerCase().includes('0xa59') && text.toLowerCase().includes('db1fa'));
        evidence.signaturePreview = text;
        await notice.screenshot({ path: `${prefix}-signature.png` });
        await confirm.click();
      }
      await pause();
    }
    assert(result && clicked, 'No final permission response; inspect wallet before retrying');
    if (result.ok) {
      assert.equal(mode, '--grant-probe');
      // Keep signatures / unspent permission contexts private; do not equate a grant with a payment.
      await writeFile(`${prefix}-private-grant.json`, JSON.stringify(result, null, 2) + '\n', { mode: 0o600 });
      assert.equal(result.value.length, 1);
      const grant = result.value[0];
      assert.equal(grant.from.toLowerCase(), user);
      assert.equal(grant.to.toLowerCase(), sponsor);
      assert.equal(BigInt(grant.chainId), BigInt(chainId));
      assert.equal(grant.permission.type, request.permission.type);
      assert.equal(BigInt(grant.permission.data.allowanceAmount), BigInt(allowanceAmount));
      assert.deepEqual(grant.rules, request.rules);
      evidence.result = { ok: true, permissionGranted: true, paymentSubmitted: false };
    } else {
      evidence.result = result;
      if (mode === '--reject') assert.equal(result.code, 4001);
      else process.exitCode = 2; // Wallet did not grant authority: feasibility is not passed.
    }
  }
  evidence.after = await chainState();
  assert.equal(evidence.after.balance, evidence.before.balance, 'Unexpected user ETH change');
  assert.equal(evidence.after.sponsorBalance, evidence.before.sponsorBalance, 'Unexpected platform ETH change');
  assert.equal(evidence.after.sponsorNonce, evidence.before.sponsorNonce, 'Unexpected platform transaction');
} catch (error) { evidence.error = error.message; process.exitCode = 1; }
finally {
  evidence.finishedAt = new Date().toISOString();
  await writeFile(`${prefix}.json`, JSON.stringify(evidence, null, 2) + '\n', { mode: 0o600 });
  await context?.close();
  await new Promise(resolve => server.close(resolve));
  console.log(JSON.stringify({ mode, result: evidence.result, error: evidence.error, evidenceFile: `${prefix}.json` }));
}
