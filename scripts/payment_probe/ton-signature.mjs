import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { Address, Cell, WalletContractV5R1, beginCell, contractAddress, loadMessageRelaxed, loadStateInit } from '@ton/ton';
import { signVerify } from '@ton/crypto';
import { loadOutListExtendedV5R1, storeOutListExtendedV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1Actions.js';
import { storeWalletIdV5R1 } from '@ton/ton/dist/wallets/v5r1/WalletV5R1WalletId.js';

// Audits the real Android wallet's one-nanoton self-transfer probe. No private key,
// RPC, signing or broadcast. This is not a production relay authorization check.
const [capturePath, reportPath] = process.argv.slice(2);
assert(capturePath && reportPath, 'Usage: node ton-signature.mjs PRIVATE_CAPTURE PUBLIC_REPORT');
const capture = JSON.parse(await readFile(capturePath, 'utf8'));
const { account, request, result } = capture;
assert.equal(account.chain, '-3');
assert.equal(request.network, '-3');
assert.equal(request.messages.length, 1);
assert.equal(request.messages[0].amount, '1');
const address = Address.parse(account.address);
assert(Address.parse(request.messages[0].address).equals(address));
const publicKey = Buffer.from(account.publicKey, 'hex');
assert.equal(publicKey.length, 32);
const wallet = WalletContractV5R1.create({ publicKey, walletId: { networkGlobalId: -3 } });
assert(wallet.address.equals(address), 'Unexpected testnet W5 address/public key');
const stateInit = loadStateInit(Cell.fromBase64(account.walletStateInit).beginParse());
assert(contractAddress(0, stateInit).equals(address));
assert(stateInit.code.equals(wallet.init.code));
assert(stateInit.data.equals(wallet.init.data));

const wrapper = loadMessageRelaxed(Cell.fromBase64(result.internalBoc).beginParse());
assert.equal(wrapper.info.type, 'internal');
assert(wrapper.info.dest.equals(address), 'Unexpected relay destination');
assert.equal(wrapper.info.value.coins, 0n);
if (wrapper.init) assert(contractAddress(0, wrapper.init).equals(address));
const slice = wrapper.body.beginParse();
const builder = beginCell().storeBits(slice.loadBits(slice.remainingBits - 512));
const signature = slice.loadBuffer(64);
while (slice.remainingRefs) builder.storeRef(slice.loadRef());
const unsigned = builder.endCell();
assert(signVerify(unsigned.hash(), signature, publicKey), 'Invalid wallet signature');
const body = unsigned.beginParse();
const opcode = body.loadUint(32);
const walletId = body.loadUint(32);
const validUntil = body.loadUint(32);
const seqno = body.loadUint(32);
assert.equal(opcode, WalletContractV5R1.OpCodes.auth_signed_internal);
assert.equal(walletId, beginCell().store(storeWalletIdV5R1(wallet.walletId)).endCell().beginParse().loadUint(32));
const actions = loadOutListExtendedV5R1(body);
assert.equal(body.remainingBits, 0);
assert.equal(body.remainingRefs, 0);
assert.equal(actions.length, 1, 'Unexpected additional actions/extensions');
const action = actions[0];
assert.equal(action.type, 'sendMsg');
assert.equal(action.mode, 3, 'Unexpected send mode');
assert.equal(action.outMsg.info.type, 'internal');
assert(action.outMsg.info.dest.equals(address));
assert.equal(action.outMsg.info.value.coins, 1n);
assert.equal(action.outMsg.init, null);
assert.equal(action.outMsg.body.bits.length, 0);
assert.equal(action.outMsg.body.refs.length, 0);

// Rebuild the real unsigned message before altering one field at a time.
function rebuild(overrides = {}) {
  return beginCell().storeUint(overrides.opcode ?? opcode, 32)
    .storeUint(overrides.walletId ?? walletId, 32)
    .storeUint(overrides.validUntil ?? validUntil, 32).storeUint(seqno, 32)
    .store(storeOutListExtendedV5R1(overrides.actions ?? actions)).endCell();
}
assert(rebuild().equals(unsigned), 'Decoded/rebuilt message differs');
const changedAmount = [{ ...action, outMsg: { ...action.outMsg, info: { ...action.outMsg.info, value: { ...action.outMsg.info.value, coins: 2n } } } }];
const changedAddress = [{ ...action, outMsg: { ...action.outMsg, info: { ...action.outMsg.info, dest: new Address(0, Buffer.alloc(32)) } } }];
const mutations = {
  amount: rebuild({ actions: changedAmount }),
  destination: rebuild({ actions: changedAddress }),
  deadline: rebuild({ validUntil: validUntil - 1 }),
  networkWalletId: rebuild({ walletId: walletId ^ 1 }),
  authType: rebuild({ opcode: WalletContractV5R1.OpCodes.auth_signed_external }),
};
for (const [name, changed] of Object.entries(mutations)) {
  assert.equal(signVerify(changed.hash(), signature, publicKey), false, `${name} tampering accepted`);
}
const now = Math.floor(Date.now() / 1000);
const deadlineMatches = validUntil === request.validUntil;
const report = {
  observedAt: new Date().toISOString(), capturedAt: capture.observedAt,
  scope: 'Real wallet response audit only; no RPC or broadcast',
  wallet: address.toRawString(), publicKey: account.publicKey, network: account.chain,
  wrapperHasStateInit: Boolean(wrapper.init), bodyHash: wrapper.body.hash().toString('hex'),
  opcode: `0x${opcode.toString(16)}`, walletId, seqno,
  signedValidUntil: validUntil, requestedValidUntil: request.validUntil,
  amountNano: '1', destination: action.outMsg.info.dest.toRawString(), sendMode: action.mode,
  checks: { walletAndStateInitMatch: true, signatureVerified: true, exactSelfTransfer: true,
    tamperingRejected: Object.keys(mutations), deadlineMatches, signedDeadlineStillFuture: validUntil > now },
  relayAllowed: false,
  reasons: [...(!deadlineMatches ? ['Signed deadline differs from requested deadline'] : []),
    'This audit does not verify current chain seqno, deployment or relay funding'],
  broadcast: false,
};
await writeFile(reportPath, JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
console.log(JSON.stringify(report, null, 2));
