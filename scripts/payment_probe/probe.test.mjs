import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import vm from 'node:vm';
import { Address } from '@ton/ton';
import { toUserFriendlyAddress } from '@tonconnect/sdk';

const source = await readFile(new URL('./probe.js', import.meta.url), 'utf8');
const rawAddress = `0:${'01'.repeat(32)}`;
async function exercise({ chain = '-3', supported = true, reject = false } = {}) {
  const calls = [], events = [];
  const elements = Object.fromEntries(['result', 'connect', 'sign', 'disconnect'].map(id => [id, {}]));
  class TonConnect {
    account = { chain, address: rawAddress };
    wallet = { device: { features: supported ? [{ name: 'SignMessage' }] : ['SendTransaction'] } };
    onStatusChange() {}
    async signMessage(request) {
      calls.push(request);
      if (reject) throw new Error('User rejected');
      return { internalBoc: 'private-executable-test-marker' };
    }
  }
  const context = vm.createContext({
    document: { getElementById: id => elements[id] }, window: {},
    location: { search: '', origin: 'http://localhost', href: 'http://localhost/' },
    navigator: { userAgent: 'isolated-unit-test' }, URLSearchParams, AbortSignal,
    TonConnectSDK: { TonConnect, toUserFriendlyAddress },
    fetch: async (_path, options) => { events.push(JSON.parse(options.body)); },
  });
  vm.runInContext(source, context);
  await elements.sign.onclick();
  return { calls, events, probe: context.window.paymentProbe };
}

test('unsupported wallets never receive a signing request', async () => {
  const r = await exercise({ supported: false });
  assert.equal(r.calls.length, 0);
  assert.equal(r.events.at(-1).type, 'sign-unsupported');
});
test('mainnet cannot reach the sign-only test request', async () => {
  const r = await exercise({ chain: '-239' });
  assert.equal(r.calls.length, 0);
  assert.match(r.events.at(-1).value.message, /testnet/);
});
test('testnet request uses a friendly self address and keeps authorization out of events', async () => {
  const r = await exercise();
  assert.equal(r.calls.length, 1);
  const request = r.calls[0];
  const friendly = Address.parseFriendly(request.messages[0].address);
  assert.equal(friendly.isTestOnly, true);
  assert.equal(friendly.address.toRawString(), rawAddress);
  assert.equal(request.messages[0].amount, '1');
  assert.equal(request.network, '-3');
  assert(request.validUntil > Date.now() / 1000);
  assert(request.validUntil <= Date.now() / 1000 + 120);
  assert.equal(r.probe.signedCapture.result.internalBoc, 'private-executable-test-marker');
  assert.equal(JSON.stringify(r.events).includes('private-executable-test-marker'), false);
});
test('wallet rejection leaves no signed capture', async () => {
  const r = await exercise({ reject: true });
  assert.equal(r.probe.signedCapture, undefined);
  assert.equal(r.events.at(-1).type, 'sign-error');
});
