// Detached signer for four fixed Devnet payouts from the funded test fixture.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { Keypair, PublicKey, TransactionMessage, VersionedTransaction } from '@solana/web3.js';
import {
  getAssociatedTokenAddressSync, createAssociatedTokenAccountIdempotentInstruction, createTransferCheckedInstruction,
} from '@solana/spl-token';
import bs58 from 'bs58';
import { USDC, USDT_FIXTURE } from './solana-token-assets.mjs';

const [preparedFile, keyFile, signatureFile] = process.argv.slice(2);
const row = JSON.parse(await readFile(preparedFile, 'utf8'));
const asset = [USDC, USDT_FIXTURE].find(a => a.mint === row.Mint);
assert(asset, 'Unsupported test mint');
assert.equal(row.AssetId, asset === USDC ? 'solana-devnet:usdc' : 'solana-devnet:usdt:' + asset.mint);
assert.equal(row.State, 'prepared');
assert.equal(row.Sender, '9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf');
assert.equal(row.TokenAtoms, '1000');
assert.equal(row.Lamports, '0');
assert([0, 1].includes(row.CreateAta));
const mode = row.CreateAta ? 'new' : 'existing';
assert.equal(row.RequestKey, 'spl-devnet-' + asset.symbol.toLowerCase() + '-' + mode + '-01');
assert.equal(row.Recipient, row.CreateAta
  ? 'DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN'
  : '7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5');
const mint = new PublicKey(asset.mint), payer = new PublicKey(row.Sender), owner = new PublicKey(row.Recipient);
const source = getAssociatedTokenAddressSync(mint, payer), dest = getAssociatedTokenAddressSync(mint, owner);
const instructions = row.CreateAta ? [createAssociatedTokenAccountIdempotentInstruction(payer, dest, owner, mint)] : [];
instructions.push(createTransferCheckedInstruction(source, mint, dest, payer, BigInt(row.TokenAtoms), asset.decimals));
const message = new TransactionMessage({payerKey: payer, recentBlockhash: row.Blockhash, instructions}).compileToLegacyMessage();
assert.equal(createHash('sha256').update(message.serialize()).digest('hex'), row.MessageHash);
const key = Keypair.fromSecretKey(Uint8Array.from(JSON.parse(await readFile(keyFile, 'utf8')).user));
assert(key.publicKey.equals(payer));
const tx = new VersionedTransaction(message);
tx.sign([key]);
await writeFile(signatureFile, bs58.encode(tx.signatures[0]), {mode: 0o600, flag: 'wx'});
