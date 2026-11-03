import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { Address, Cell, beginCell, loadMessageRelaxed, storeMessageRelaxed } from '@ton/ton';
import { validatePayment, directory } from './ton-live.mjs';

// Check an actual wallet capture before relaying. No key access, RPC or broadcast.
const capture = JSON.parse(await readFile(process.argv[2], 'utf8'));
const now = Math.floor(Date.now() / 1000), seqno = capture.before.user.seqno;
const auth = validatePayment(capture, seqno, now);
const results = [];
function rejected(name, change, expected) {
  const copy = structuredClone(capture); change(copy);
  assert.throws(() => validatePayment(copy, seqno, now), expected);
  results.push(name);
}
rejected('wrong network', c => { c.account.chain = '-239'; });
rejected('changed requested amount', c => { c.request.messages[0].amount = '1'; });
rejected('changed requested recipient', c => { c.request.messages[0].address = c.account.address; });
rejected('changed requested deadline', c => { c.request.validUntil -= 1; }, /deadline/);
rejected('changed relay destination', c => {
  const m = loadMessageRelaxed(Cell.fromBase64(c.result.internalBoc).beginParse());
  m.info.dest = new Address(0, Buffer.alloc(32));
  c.result.internalBoc = beginCell().store(storeMessageRelaxed(m)).endCell().toBoc().toString('base64');
}, /relay destination/);
rejected('modified signed message', c => {
  const m = loadMessageRelaxed(Cell.fromBase64(c.result.internalBoc).beginParse());
  const s = m.body.beginParse(), b = beginCell().storeUint(s.loadUint(32) ^ 1, 32).storeSlice(s);
  m.body = b.endCell();
  c.result.internalBoc = beginCell().store(storeMessageRelaxed(m)).endCell().toBoc().toString('base64');
}, /Invalid signature/);
assert.throws(() => validatePayment(capture, seqno + 1, now), /Stale wallet seqno/);
results.push('stale seqno');
assert.throws(() => validatePayment(capture, seqno, auth.validUntil + 1), /Expired/);
results.push('expired authorization');
const report = { observedAt: new Date().toISOString(), captureAt: capture.observedAt, realWallet: capture.device,
  signatureVerified: true, seqno, validUntil: auth.validUntil, requestedValidUntil: capture.request.validUntil,
  bodyHash: auth.bodyHash, rejectedCases: results, broadcast: false };
await writeFile(new URL(`preflight-${Date.now()}.json`, directory), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
console.log(JSON.stringify(report, null, 2));
