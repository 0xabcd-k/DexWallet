import { createApiClient, decodeHealth } from '@dexwallet/api-client';
export const api = createApiClient({ baseUrl: import.meta.env.VITE_API_BASE_URL ?? '/api' });
export const getHealth = (signal?: AbortSignal) => api.request('/health/live', decodeHealth, signal ? { signal } : {});
