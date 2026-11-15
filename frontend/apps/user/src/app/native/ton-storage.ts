import { Preferences } from '@capacitor/preferences';
import type { IStorage } from '@tonconnect/sdk';

export interface ConnectionStorage {
  get(options: { key: string }): Promise<{ value: string | null }>;
  set(options: { key: string; value: string }): Promise<void>;
  remove(options: { key: string }): Promise<void>;
  keys(): Promise<{ keys: string[] }>;
}
// Native preferences hold protocol session material, never wallet seeds. Account
// scoping prevents an account switch from adopting another user's connection.
export function tonStorage(userIdx: string, storage: ConnectionStorage = Preferences): IStorage & { clear(): Promise<void>; close(): void } {
  if (!userIdx) throw new Error('A product account is required');
  const prefix = `dexwallet:ton-connect:${userIdx}:`;
  let closed = false;
  return {
    async getItem(key) { return (await storage.get({ key: prefix + key })).value; },
    async setItem(key, value) { if (!closed) await storage.set({ key: prefix + key, value }); },
    async removeItem(key) { if (!closed) await storage.remove({ key: prefix + key }); },
    async clear() {
      const { keys } = await storage.keys();
      await Promise.all(keys.filter(key => key.startsWith(prefix)).map(key => storage.remove({ key })));
    },
    close() { closed = true; },
  };
}
