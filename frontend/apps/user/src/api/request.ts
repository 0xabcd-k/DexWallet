import { createApiClient, decodeHealth } from '@dexwallet/api-client';
import { flushNativeStorage } from '../app/native/state-runtime';
export const api = createApiClient({ ...(import.meta.env.NATIVE_APP ? { fetch: async (...args: Parameters<typeof fetch>) => { await flushNativeStorage(); return globalThis.fetch(...args); } } : {}), baseUrl: import.meta.env.NATIVE_APP ? import.meta.env.APP_API_URL : import.meta.env.VITE_API_BASE_URL ?? '/api' });
export const getHealth = (signal?: AbortSignal) => api.request('/health/live', decodeHealth, signal ? { signal } : {});
