import { retrieveRawLaunchParams } from '@tma.js/bridge';
import { isRecord } from '@dexwallet/api-client';
import { randomToken } from '../random';

export const LAUNCH_STORAGE_KEY = 'dexwallet:user:launch';
export type TargetStatus = 'pending' | 'consumed' | 'invalid';
interface SavedLaunch {
  fingerprint: string;
  launchId: string;
  target: string | null;
  targetStatus: TargetStatus;
}
export interface LaunchContext extends SavedLaunch {
  mode: 'browser' | 'telegram' | 'error';
  source: 'none' | 'fresh' | 'restored';
  rawInitData: string;
  language?: string | undefined;
  storageAvailable: boolean;
}
interface BootstrapOptions {
  href: string;
  navigationType: string;
  storage: Pick<Storage, 'getItem' | 'setItem'> | undefined;
  retrieve?: () => string;
}
const empty: SavedLaunch = { fingerprint: '', launchId: '', target: null, targetStatus: 'consumed' };
function readSaved(storage: BootstrapOptions['storage']): SavedLaunch | undefined {
  try {
    const value: unknown = JSON.parse(storage?.getItem(LAUNCH_STORAGE_KEY) ?? 'null');
    if (isRecord(value) && typeof value.fingerprint === 'string' && typeof value.launchId === 'string' &&
        (value.target === null || typeof value.target === 'string') &&
        (value.targetStatus === 'pending' || value.targetStatus === 'consumed' || value.targetStatus === 'invalid')) {
      return { fingerprint: value.fingerprint, launchId: value.launchId, target: value.target, targetStatus: value.targetStatus };
    }
  } catch { /* Missing/unavailable storage is not a valid launch. */ }
}
function save(value: SavedLaunch, storage: BootstrapOptions['storage']): boolean {
  if (!storage) return false;
  try {
    storage.setItem(LAUNCH_STORAGE_KEY, JSON.stringify({ fingerprint: value.fingerprint, launchId: value.launchId, target: value.target, targetStatus: value.targetStatus }));
    return true;
  } catch { return false; }
}
function targetFrom(init: URLSearchParams, query: URLSearchParams): { target: string | null; targetStatus: TargetStatus } {
  const signedCandidate = init.get('start_param'); // Unverified until phase 2 exchanges rawInitData with the API.
  const queryCandidate = query.get('tgWebAppStartParam');
  const target = signedCandidate ?? queryCandidate;
  if (signedCandidate !== null && queryCandidate !== null && signedCandidate !== queryCandidate) return { target: null, targetStatus: 'invalid' };
  if (target !== null && !/^[A-Za-z0-9_-]{1,512}$/.test(target)) return { target: null, targetStatus: 'invalid' };
  return { target, targetStatus: target === null ? 'consumed' : 'pending' };
}
export async function bootstrapTelegram(options: BootstrapOptions): Promise<LaunchContext> {
  const url = new URL(options.href);
  const fragment = new URLSearchParams(url.hash.slice(1));
  const fresh = fragment.has('tgWebAppVersion') || fragment.has('tgWebAppData');
  const routeHash = /^#\/(?!\/)/.test(url.hash);
  const saved = readSaved(options.storage);
  const restoring = !fresh && routeHash && (options.navigationType === 'reload' || options.navigationType === 'back_forward');
  const base: LaunchContext = { ...empty, mode: 'browser', source: 'none', rawInitData: '', storageAvailable: !!options.storage };
  if (!fresh && !(restoring && saved)) return base;
  try {
    // The SDK owns raw launch caching. Our storage contains only target lifecycle metadata.
    const params = new URLSearchParams((options.retrieve ?? retrieveRawLaunchParams)());
    const rawInitData = params.get('tgWebAppData');
    if (!rawInitData || !params.get('tgWebAppVersion') || !params.get('tgWebAppPlatform')) throw new Error('Missing Telegram launch data');
    if (fresh && (rawInitData !== fragment.get('tgWebAppData') || params.get('tgWebAppVersion') !== fragment.get('tgWebAppVersion') || params.get('tgWebAppPlatform') !== fragment.get('tgWebAppPlatform'))) {
      throw new Error('SDK recovered stale data for a fresh launch');
    }
    const init = new URLSearchParams(rawInitData);
    const bytes = new TextEncoder().encode(rawInitData);
    const digest = import.meta.env.DEV && !crypto.subtle
      ? (await import('../../dev/sha256')).sha256(bytes)
      : new Uint8Array(await crypto.subtle.digest('SHA-256', bytes));
    const fingerprint = Array.from(digest, v => v.toString(16).padStart(2, '0')).join('');
    // This fingerprint correlates a cache with a launch; it is NOT authentication.
    if (restoring && saved?.fingerprint !== fingerprint) throw new Error('Launch recovery mismatch');
    let language: string | undefined;
    const user: unknown = JSON.parse(init.get('user') ?? 'null');
    if (isRecord(user) && typeof user.language_code === 'string') language = user.language_code;
    const lifecycle = fresh ? { fingerprint, launchId: randomToken(), ...targetFrom(init, url.searchParams) } : saved;
    if (!lifecycle) throw new Error('Missing launch lifecycle');
    const context: LaunchContext = { ...lifecycle, mode: 'telegram', source: fresh ? 'fresh' : 'restored', rawInitData, language, storageAvailable: false };
    context.storageAvailable = save(context, options.storage);
    return context;
  } catch {
    // SDK errors may contain raw initData. They must not reach a log or public error screen.
    return { ...base, mode: 'error', source: fresh ? 'fresh' : 'restored' };
  }
}
export function updateTarget(context: LaunchContext, status: TargetStatus, storage: BootstrapOptions['storage']): LaunchContext {
  const updated = { ...context, targetStatus: status };
  return { ...updated, storageAvailable: save(updated, storage) };
}
export function sessionStorageOrUndefined(): Storage | undefined {
  try { return window.sessionStorage; } catch { return undefined; }
}
