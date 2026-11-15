import { expect, it, vi } from 'vitest';
import { createNativeStorage } from '../../apps/user/src/app/native/storage';
import type { ConnectionStorage } from '../../apps/user/src/app/native/ton-storage';
function backend() {
  const values = new Map<string,string>([['foreign-key','keep']]);
  const storage: ConnectionStorage = {
    async get({key}) { return {value:values.get(key) ?? null}; },
    async set({key,value}) { values.set(key,value); },
    async remove({key}) { values.delete(key); },
    async keys() { return {keys:[...values.keys()]}; },
  };
  return {values,storage};
}
it('restores credentials and original requests from native storage after a restart', async () => {
  const b = backend(); const first = await createNativeStorage(b.storage);
  first.storage.setItem('session','test-token'); first.storage.setItem('pending-payment','original-key');
  await first.flush();
  const restarted = await createNativeStorage(b.storage);
  expect(restarted.storage.getItem('session')).toBe('test-token');
  expect(restarted.storage.getItem('pending-payment')).toBe('original-key');
  restarted.storage.clear(); await restarted.flush();
  expect([...b.values.keys()]).toEqual(['foreign-key']);
});
it('does not pass the persistence barrier until a pending request is written', async () => {
  const b = backend(); let finish!: () => void;
  b.storage.set = () => new Promise(resolve => { finish = resolve; });
  const store = await createNativeStorage(b.storage); const submit = vi.fn();
  store.storage.setItem('pending-payment','original-key');
  const flow = store.flush().then(submit); await Promise.resolve();
  expect(submit).not.toHaveBeenCalled(); finish(); await flow;
  expect(submit).toHaveBeenCalledOnce();
});
it('blocks subsequent requests after a native write failure instead of reporting success', async () => {
  const b = backend(); b.storage.set = async () => { throw new Error('disk unavailable'); };
  const store = await createNativeStorage(b.storage);
  store.storage.setItem('pending-payment','original-key');
  await expect(store.flush()).rejects.toThrow('disk unavailable');
  await expect(store.flush()).rejects.toThrow('disk unavailable');
});
