// Dedicated self-issued Devnet fixture. Never creates/transacts a mainnet asset.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { Keypair, PublicKey, SystemProgram, Transaction } from '@solana/web3.js';
import {
  MINT_SIZE, TOKEN_PROGRAM_ID, createInitializeMint2Instruction,
  createAssociatedTokenAccountIdempotentInstruction, createMintToCheckedInstruction,
  createFreezeAccountInstruction, createThawAccountInstruction,
} from '@solana/spl-token';
import bs58 from 'bs58';
import { rpc } from './rpc.mjs';
import { DEVNET_ENDPOINT, MAX_FEE_LAMPORTS, digest } from './solana-live-core.mjs';
import { tokenAddresses } from './solana-token-core.mjs';
import { USDT_FIXTURE as asset, MAINNET_USDT_MINT } from './solana-token-assets.mjs';

const mode = process.argv[2] ?? '--inspect';
assert(process.argv.length <= 3 && ['--inspect', '--setup', '--freeze', '--thaw'].includes(mode));
const output = fileURLToPath(new URL('../../.tools/payment-feasibility/', import.meta.url));
const addresses = JSON.parse(await readFile(`${output}/solana-live-addresses.json`, 'utf8'));
const atas = tokenAddresses(addresses, asset);
const mint = new PublicKey(asset.mint), userAta = new PublicKey(atas.user);
const accounts = async (endpoint, keys) => rpc(endpoint, 'getMultipleAccounts', [keys, { commitment: 'finalized', encoding: 'jsonParsed' }]);

if (mode === '--inspect') {
  const mainnet = await accounts('https://api.mainnet-beta.solana.com', [MAINNET_USDT_MINT]);
  assert.equal(mainnet.value[0].owner, TOKEN_PROGRAM_ID.toBase58());
  assert.equal(mainnet.value[0].data.parsed.info.decimals, asset.decimals);
  const devnet = await accounts(DEVNET_ENDPOINT, [MAINNET_USDT_MINT, asset.mint, atas.user, atas.recipient]);
  const programs = {};
  for (const [name, endpoint] of [['mainnet', 'https://api.mainnet-beta.solana.com'], ['devnet', DEVNET_ENDPOINT]]) {
    const account = await rpc(endpoint, 'getAccountInfo', [TOKEN_PROGRAM_ID.toBase58(), { encoding: 'base64', commitment: 'finalized' }]);
    assert.equal(account.value.executable, true);
    assert.equal(account.value.owner, 'BPFLoaderUpgradeab1e11111111111111111111111');
    const bytes = Buffer.from(account.value.data[0], 'base64');
    assert.equal(bytes.readUInt32LE(0), 2); // UpgradeableLoaderState::Program.
    const programDataAddress = new PublicKey(bytes.subarray(4, 36)).toBase58();
    const programData = await rpc(endpoint, 'getAccountInfo', [programDataAddress, { encoding: 'base64', commitment: 'finalized' }]);
    const data = Buffer.from(programData.value.data[0], 'base64');
    assert.equal(data.readUInt32LE(0), 3); // UpgradeableLoaderState::ProgramData.
    const PROGRAM_DATA_METADATA_SIZE = 45; // u32 tag, u64 slot, Option<Pubkey>.
    programs[name] = { slot: programData.context.slot, owner: account.value.owner, programDataAddress,
      dataSha256: digest(bytes), deployedSlot: data.readBigUInt64LE(4).toString(),
      codeLength: data.length - PROGRAM_DATA_METADATA_SIZE, codeSha256: digest(data.subarray(PROGRAM_DATA_METADATA_SIZE)) };
  }
  const evidence = { observedAt: new Date().toISOString(), officialSource: 'https://tether.to/en/supported-protocols/',
    mainnetMint: MAINNET_USDT_MINT, mainnet, devnetKeys: [MAINNET_USDT_MINT, asset.mint, atas.user, atas.recipient], devnet, programs,
    identicalProgramAccountBytes: programs.mainnet.dataSha256 === programs.devnet.dataSha256,
    identicalProgramCode: programs.mainnet.codeSha256 === programs.devnet.codeSha256,
    fixture: { mint: asset.mint, label: asset.description, issuer: addresses.sponsor, tetherIssued: false }, addresses, tokenAccounts: atas };
  await writeFile(`${output}/solana-usdt-identity.json`, JSON.stringify(evidence, null, 2));
  console.log(JSON.stringify({ mainnetMint: MAINNET_USDT_MINT, fixtureMint: asset.mint,
    sameAddressOnDevnetType: devnet.value[0]?.data?.parsed?.type ?? 'not a parsed token account',
    programs, identicalProgramCode: evidence.identicalProgramCode }, null, 2));
} else {
  // Setup/admin reads the platform key and fixture mint key only, never the wallet user's key.
  const sponsor = Keypair.fromSecretKey(Uint8Array.from(JSON.parse(await readFile(`${output}/solana-sponsor-only.json`, 'utf8'))));
  assert.equal(sponsor.publicKey.toBase58(), addresses.sponsor);
  const statePath = `${output}/solana-usdt-fixture-${mode.slice(2)}-admin-private.json`;
  let state;
  try { state = JSON.parse(await readFile(statePath, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  if (!state) {
    const before = await accounts(DEVNET_ENDPOINT, [asset.mint, atas.user, atas.recipient, addresses.sponsor]);
    const block = (await rpc(DEVNET_ENDPOINT, 'getLatestBlockhash', [{ commitment: 'confirmed' }])).value;
    const tx = new Transaction({ feePayer: sponsor.publicKey, recentBlockhash: block.blockhash });
    const signers = [sponsor];
    if (mode === '--setup') {
      assert.equal(before.value[0], null, 'Fixture mint exists; do not issue more tokens');
      const mintKey = Keypair.fromSecretKey(Uint8Array.from(JSON.parse(await readFile(`${output}/solana-usdt-fixture-mint-key.json`, 'utf8'))));
      assert.equal(mintKey.publicKey.toBase58(), asset.mint);
      const rent = await rpc(DEVNET_ENDPOINT, 'getMinimumBalanceForRentExemption', [MINT_SIZE, { commitment: 'confirmed' }]);
      assert(rent > 0 && rent <= 3000000);
      tx.add(
        SystemProgram.createAccount({ fromPubkey: sponsor.publicKey, newAccountPubkey: mint, space: MINT_SIZE, lamports: rent, programId: TOKEN_PROGRAM_ID }),
        createInitializeMint2Instruction(mint, asset.decimals, sponsor.publicKey, sponsor.publicKey),
        createAssociatedTokenAccountIdempotentInstruction(sponsor.publicKey, userAta, new PublicKey(addresses.user), mint),
        createMintToCheckedInstruction(mint, userAta, sponsor.publicKey, 20000000n, asset.decimals),
      );
      signers.push(mintKey);
    } else {
      assert.equal(before.value[0].data.parsed.info.freezeAuthority, addresses.sponsor);
      assert.equal(before.value[1].data.parsed.info.state, mode === '--freeze' ? 'initialized' : 'frozen');
      tx.add((mode === '--freeze' ? createFreezeAccountInstruction : createThawAccountInstruction)(userAta, mint, sponsor.publicKey));
    }
    tx.sign(...signers);
    const fee = (await rpc(DEVNET_ENDPOINT, 'getFeeForMessage', [tx.serializeMessage().toString('base64'), { commitment: 'confirmed' }])).value;
    assert(fee !== null && fee <= MAX_FEE_LAMPORTS);
    const signed = tx.serialize().toString('base64');
    const simulation = await rpc(DEVNET_ENDPOINT, 'simulateTransaction', [signed, { encoding: 'base64', sigVerify: true, commitment: 'confirmed' }]);
    assert.equal(simulation.value.err, null);
    state = { mode, mint: asset.mint, addresses, tokenAccounts: atas, before, block, fee, simulation, signature: bs58.encode(tx.signature), signed, preparedAt: new Date().toISOString() };
    await writeFile(statePath, JSON.stringify(state, null, 2), { mode: 0o600, flag: 'wx' });
    const accepted = await rpc(DEVNET_ENDPOINT, 'sendTransaction', [signed, { encoding: 'base64', skipPreflight: false, preflightCommitment: 'confirmed', maxRetries: 0 }]);
    assert.equal(accepted, state.signature);
  }
  // An existing state is inspected, never replaced by another signature after an uncertain response.
  for (let attempt = 0; attempt < 30; attempt++) {
    state.chainStatus = (await rpc(DEVNET_ENDPOINT, 'getSignatureStatuses', [[state.signature], { searchTransactionHistory: true }])).value[0];
    if (state.chainStatus?.err || state.chainStatus?.confirmationStatus === 'finalized') break;
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  assert.equal(state.chainStatus?.err, null);
  assert.equal(state.chainStatus.confirmationStatus, 'finalized', 'Inspect the stored transaction; do not replace it');
  state.receipt = await rpc(DEVNET_ENDPOINT, 'getTransaction', [state.signature, { encoding: 'json', commitment: 'finalized', maxSupportedTransactionVersion: 0 }]);
  assert.equal(state.receipt.meta.err, null);
  state.after = await accounts(DEVNET_ENDPOINT, [asset.mint, atas.user, atas.recipient, addresses.sponsor]);
  state.observedAt = new Date().toISOString();
  await writeFile(statePath, JSON.stringify(state, null, 2), { mode: 0o600 });
  const { signed, ...publicResult } = state;
  await writeFile(`${output}/solana-usdt-fixture-${mode.slice(2)}-admin.json`, JSON.stringify(publicResult, null, 2));
  console.log(JSON.stringify({ mode, signature: state.signature, status: state.chainStatus.confirmationStatus, mint: asset.mint, sourceState: state.after.value[1].data.parsed.info.state, sourceAmount: state.after.value[1].data.parsed.info.tokenAmount.amount, fee: state.receipt.meta.fee }, null, 2));
}
