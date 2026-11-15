import { Buffer as BrowserBuffer } from 'buffer';
// Use the same Buffer implementation as TON's SDK, including in Node tests.
export const Buffer = typeof globalThis.Buffer === 'undefined' ? BrowserBuffer : globalThis.Buffer;
if (!('Buffer' in globalThis)) Object.defineProperty(globalThis, 'Buffer', { value: Buffer, configurable: true });
