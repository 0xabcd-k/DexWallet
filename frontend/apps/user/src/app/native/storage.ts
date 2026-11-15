import { registerPlugin } from '@capacitor/core';
import type { ConnectionStorage } from './ton-storage';
import { installNativeFlush } from './state-runtime';

const webPrefix = 'dexwallet:web:';
const NativeState = registerPlugin<ConnectionStorage>('NativeState');

// Existing page code reads Web Storage synchronously. Hydrate a native-backed
// cache first; explicit barriers below await writes before auth completion and
// before any API request, including a payment with a newly saved request key.
export async function createNativeStorage(backend: ConnectionStorage) {
  const values = new Map<string, string>();
  const { keys } = await backend.keys();
  await Promise.all(keys.filter(key => key.startsWith(webPrefix)).map(async key => {
    const { value } = await backend.get({ key });
    if (value !== null) values.set(key.slice(webPrefix.length), value);
  }));
  let pending = Promise.resolve();
  function enqueue(write: () => Promise<void>) {
    pending = pending.then(write);
    // Synchronous Storage callers cannot await. flush() reports the failure;
    // subsequent API calls remain blocked instead of losing a request key.
    void pending.catch(() => {});
  }
  const storage: Storage = {
    get length() { return values.size; },
    key(index) { return [...values.keys()][index] ?? null; },
    getItem(key) { return values.get(String(key)) ?? null; },
    setItem(key, value) {
      key = String(key); value = String(value);
      values.set(key, value);
      enqueue(() => backend.set({ key: webPrefix + key, value }));
    },
    removeItem(key) {
      key = String(key); values.delete(key);
      enqueue(() => backend.remove({ key: webPrefix + key }));
    },
    clear() { for (const key of values.keys()) storage.removeItem(key); },
  };
  return { storage, async flush() {
    let batch: Promise<void>;
    do { batch = pending; await batch; } while (batch !== pending);
  } };
}
let native: Awaited<ReturnType<typeof createNativeStorage>> | undefined;
export async function installNativeStorage() {
  native = await createNativeStorage(NativeState);
  Object.defineProperty(window, 'localStorage', { configurable: true, get: () => native!.storage });
  installNativeFlush(native.flush);
}
