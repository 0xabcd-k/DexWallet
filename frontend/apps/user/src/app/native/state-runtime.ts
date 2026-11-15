let flush: (() => Promise<void>) | undefined;
export function installNativeFlush(value: () => Promise<void>) { flush = value; }
export async function flushNativeStorage() {
  if (!flush) throw new Error('Native storage was not initialized');
  await flush();
}
