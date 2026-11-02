import { createServer } from 'node:http';
import { appendFile, mkdir, readFile } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const output = resolve(process.env.PAYMENT_PROBE_OUTPUT ?? resolve(here, '../../.tools/payment-feasibility'));
const port = Number(process.env.PAYMENT_PROBE_PORT ?? 18791);
await mkdir(output, { recursive: true, mode: 0o700 });
const sdk = resolve(here, 'node_modules/@tonconnect/sdk/dist/tonconnect-sdk.min.js');
const server = createServer(async (req, res) => {
  try {
    const protocol = req.headers['x-forwarded-proto'] === 'https' ? 'https' : 'http';
    const url = new URL(req.url, `${protocol}://${req.headers.host}`);
    if (req.method === 'POST' && url.pathname === '/events') {
      let body = '';
      for await (const chunk of req) {
        body += chunk;
        if (body.length > 262144) throw new Error('Probe event too large');
      }
      const event = JSON.parse(body);
      await appendFile(resolve(output, 'wallet-events.ndjson'), JSON.stringify({ observedAt: new Date().toISOString(), ...event }) + '\n', { mode: 0o600 });
      res.writeHead(204).end();
      return;
    }
    if (req.method !== 'GET') { res.writeHead(405).end(); return; }
    if (url.pathname === '/tonconnect-manifest.json') {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ url: url.origin, name: 'DexWallet test-only probe', iconUrl: `${url.origin}/icon.png` }));
    } else if (url.pathname === '/icon.png') {
      res.setHeader('Content-Type', 'image/png');
      res.end(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGMQz/wPAAIaAYCT8mQ7AAAAAElFTkSuQmCC', 'base64'));
    } else if (url.pathname === '/sdk.js') {
      res.setHeader('Content-Type', 'application/javascript'); res.end(await readFile(sdk));
    } else if (url.pathname === '/probe.js') {
      res.setHeader('Content-Type', 'application/javascript'); res.end(await readFile(resolve(here, 'probe.js')));
    } else if (url.pathname === '/') {
      res.setHeader('Content-Type', 'text/html');
      res.end('<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><title>Payment capability probe</title><style>body{font:18px sans-serif;padding:16px}button{display:block;margin:16px 0;padding:16px;font-size:18px}pre{white-space:pre-wrap;word-break:break-word;font-size:12px}</style><h1>Test-only wallet probe</h1><p>No transaction broadcast.</p><button id="connect">Connect injected wallet</button><button id="sign">Check sign-only capability</button><button id="disconnect">Disconnect</button><pre id="result"></pre><script src="/sdk.js"></script><script src="/probe.js"></script>');
    } else { res.writeHead(404).end(); }
  } catch (error) { console.error(error); res.writeHead(500).end('Probe failed'); }
});
server.listen(port, '127.0.0.1', () => console.log(`Payment probe listening on 127.0.0.1:${port}; Android host alias 10.0.2.2`));
