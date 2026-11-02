/* Development probe only. Never calls sendTransaction or broadcasts a transaction. */
const TESTNET = '-3';
const output = document.getElementById('result');
const manifestOrigin = new URLSearchParams(location.search).get('manifestOrigin') ?? location.origin;
const connector = new TonConnectSDK.TonConnect({ manifestUrl: `${manifestOrigin}/tonconnect-manifest.json` });
window.paymentProbe = { connector };
window.paymentProbe.connectBridge = () => connector.connect({ universalLink: 'https://app.tonkeeper.com/ton-connect', bridgeUrl: 'https://bridge.tonapi.io/bridge' });

async function record(type, value) {
  const event = { type, value, page: location.href, userAgent: navigator.userAgent };
  output.textContent = JSON.stringify(event, null, 2);
  await fetch('/events', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(event) });
  window.paymentProbe.lastEvent = event;
}
function errorValue(error) { return { name: error.name, message: error.message, code: error.code }; }

connector.onStatusChange(wallet => {
  record('connection', wallet && { device: wallet.device, account: wallet.account, provider: wallet.provider });
}, error => record('connection-error', errorValue(error)));

document.getElementById('connect').onclick = async () => {
  try {
    const injected = ['tonkeeper', 'mytonwallet', 'mywallet', 'okxTonWallet'].find(key => window[key]?.tonconnect);
    if (!injected) throw new Error('No supported injected wallet provider found');
    await record('injected-provider', { key: injected, walletInfo: window[injected].tonconnect.walletInfo, deviceInfo: window[injected].tonconnect.deviceInfo });
    await connector.connect({ jsBridgeKey: injected });
  } catch (error) { await record('connect-call-error', errorValue(error)); }
};

document.getElementById('sign').onclick = async () => {
  try {
    if (!connector.wallet) throw new Error('Connect a wallet first');
    const features = connector.wallet.device.features;
    const supportsSignMessage = features.some(feature => typeof feature === 'object' && feature.name === 'SignMessage');
    await record('sign-capability', { features, supportsSignMessage, chain: connector.account.chain });
    // Missing features are rejected by the SDK before dispatch. Supported wallets must be on testnet.
    if (supportsSignMessage && connector.account.chain !== TESTNET) throw new Error('Switch the dedicated wallet to testnet before requesting a signature');
    const result = await connector.signMessage({
      network: TESTNET,
      validUntil: Math.floor(Date.now() / 1000) + 120,
      messages: [{ address: connector.account.address, amount: '1' }],
    }, { signal: AbortSignal.timeout(60000) });
    // Persist no executable authorization in public evidence.
    await record('sign-result', { internalBocReturned: typeof result.internalBoc === 'string', traceId: result.traceId });
  } catch (error) { await record('sign-error', errorValue(error)); }
};

document.getElementById('disconnect').onclick = () => connector.disconnect().catch(error => record('disconnect-error', errorValue(error)));
record('loaded', { sdkVersion: '5.0.0', injectedKeys: ['tonkeeper', 'mytonwallet', 'mywallet'].filter(key => window[key]?.tonconnect) });
