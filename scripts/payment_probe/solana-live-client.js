const display = document.getElementById('result');
window.solanaProbe = { order: null, last: null };
const show = value => { window.solanaProbe.last = value; display.textContent = JSON.stringify(value, null, 2); };
async function post(path, value) {
  const response = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(value) });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error);
  return result;
}
function action(id, handler) {
  document.getElementById(id).onclick = async () => {
    try { await handler(); } catch (error) { show({ error: error.message }); }
  };
}
action('connect', async () => {
  const wallet = document.getElementById('wallet').value;
  const provider = ({ phantom: window.phantom?.solana, solflare: window.solflare,
    backpack: window.backpack, okx: window.okxwallet?.solana })[wallet];
  if (!provider) throw new Error(`${wallet} injected provider not detected`);
  await provider.connect();
  if (!provider.publicKey) throw new Error(`${wallet} connection was not approved or could not open`);
  window.solanaProbe.provider = provider;
  window.solanaProbe.wallet = wallet;
  window.solanaProbe.user = provider.publicKey.toBase58();
  document.getElementById('sign').disabled = false;
  show({ connected: true, wallet, user: window.solanaProbe.user, signTransactionAvailable: typeof provider.signTransaction === 'function' });
});
action('sign', async () => {
  document.getElementById('submit').disabled = true;
  const order = await post('/prepare', { user: window.solanaProbe.user });
  window.solanaProbe.order = order;
  show({ status: 'waiting-wallet', order });
  const bytes = Uint8Array.from(atob(order.unsigned), character => character.charCodeAt(0));
  const signed = await window.solanaProbe.provider.signTransaction(solanaWeb3.Transaction.from(bytes));
  const encoded = btoa(String.fromCharCode(...signed.serialize({ requireAllSignatures: false })));
  const verified = await post('/validate', { reference: order.reference, transaction: encoded });
  window.solanaProbe.order = verified;
  show(verified);
  document.getElementById('submit').disabled = false;
});
action('submit', async () => {
  document.getElementById('submit').disabled = true;
  show(await post('/submit', { reference: window.solanaProbe.order.reference }));
});
action('status', async () => {
  const response = await fetch('/state');
  show(await response.json());
});
