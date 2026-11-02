import { spawn } from 'node:child_process';

// curl makes the host's explicit proxy choice reproducible; no credentials in argv.
export function post(url, body, { direct = false } = {}) {
  return new Promise((resolve, reject) => {
    const args = ['-4', '--silent', '--show-error', '--fail-with-body', '--connect-timeout', '15', '--max-time', '40'];
    if (direct) args.push('--noproxy', '*');
    args.push('-H', 'Content-Type: application/json', '--data-binary', '@-', url);
    const child = spawn('curl', args);
    let out = '', err = '';
    child.stdout.on('data', x => { out += x; });
    child.stderr.on('data', x => { err += x; });
    child.on('error', reject);
    child.on('close', code => {
      if (code !== 0) return reject(new Error(`curl ${code}: ${err.trim()}; ${out.slice(0, 800)}`));
      try { resolve(JSON.parse(out)); } catch (error) { reject(error); }
    });
    child.stdin.end(JSON.stringify(body));
  });
}

export async function rpc(url, method, params = [], options) {
  const response = await post(url, { jsonrpc: '2.0', id: 1, method, params }, options);
  if (response.error) throw new Error(JSON.stringify(response.error));
  return response.result;
}
