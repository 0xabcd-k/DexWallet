// Sign one independently pinned Devnet SOL transfer using the existing test sponsor.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { Keypair, PublicKey, SystemProgram, Transaction } from '@solana/web3.js';
import bs58 from 'bs58';

const [preparedPath, sponsorPath, signaturePath] = process.argv.slice(2);
const row = JSON.parse(await readFile(preparedPath, 'utf8'));
assert.equal(row.RequestKey, 'sol-native-devnet-01');
assert.equal(row.State, 'prepared');
assert.equal(row.Sender, 'DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN');
assert.equal(row.Recipient, '7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5');
assert.equal(row.Lamports, '1000');
const payer = new PublicKey(row.Sender);
const tx = new Transaction({ feePayer: payer, recentBlockhash: row.Blockhash });
tx.add(SystemProgram.transfer({ fromPubkey: payer, toPubkey: new PublicKey(row.Recipient), lamports: BigInt(row.Lamports) }));
assert.equal(createHash('sha256').update(tx.serializeMessage()).digest('hex'), row.MessageHash);
const signer = Keypair.fromSecretKey(Uint8Array.from(JSON.parse(await readFile(sponsorPath, 'utf8'))));
assert(signer.publicKey.equals(payer));
tx.sign(signer);
await writeFile(signaturePath, bs58.encode(tx.signature), { flag: 'wx', mode: 0o600 });
