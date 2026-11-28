import { mnemonicToSeed } from '@scure/bip39';
import { nativeVault } from '../local-wallet/bridge';
import { parseMaterial, WalletError } from '../local-wallet/crypto';

export async function signWithSeed(userIdx: string, walletId: string, signal: AbortSignal, sign: (seed: Uint8Array) => string) {
  const token = crypto.randomUUID();
  const close = () => { void nativeVault.unprotect({ token }).catch(() => {}); };
  signal.throwIfAborted();
  const listener = await nativeVault.addListener('privacyChange', state => {
    if (state.token === token && state.active && !signal.aborted) void nativeVault.acknowledge({ token, revision: state.revision }).catch(() => {});
  });
  try {
    signal.throwIfAborted();
    signal.addEventListener('abort', close, { once: true });
    const state = await nativeVault.protect({ token });
    if (!state.active) throw new WalletError('LOCAL_VIEW_CLOSED');
    await nativeVault.acknowledge({ token, revision: state.revision });
    signal.throwIfAborted();
    const { secret } = await nativeVault.get({ userIdx, walletId, viewToken: token });
    const material = await parseMaterial(secret);
    if (material.scheme !== 'bip39') throw new WalletError('LOCAL_INVALID_SECRET');
    const seed = await mnemonicToSeed(material.words, '');
    try {
      signal.throwIfAborted();
      if (document.visibilityState === 'hidden') throw new WalletError('LOCAL_VIEW_CLOSED');
      return sign(seed);
    } finally { seed.fill(0); }
  } finally {
    signal.removeEventListener('abort', close);
    try { await nativeVault.unprotect({ token }); } finally { await listener.remove(); }
  }
}
