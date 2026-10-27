import type { HealthResponse } from '@dexwallet/api-contract/platform';

export type ErrorKind = 'business' | 'http' | 'network' | 'timeout' | 'cancelled' | 'protocol';
export class ApiError extends Error {
  constructor(
    readonly kind: ErrorKind,
    readonly status?: number,
    readonly code?: string,
    readonly traceId?: string,
    options?: ErrorOptions,
  ) {
    super(code ?? kind, options);
    this.name = 'ApiError';
  }
}
export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
export type Decoder<T> = (value: unknown) => T;
export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
  timeoutMs?: number;
}
export interface ClientOptions {
  baseUrl: string;
  timeoutMs?: number;
  fetch?: typeof globalThis.fetch;
}
const DEFAULT_TIMEOUT_MS = 10_000;
function checkedTimeout(value: number): number {
  if (!Number.isFinite(value) || value <= 0 || value > 2_147_483_647) {
    throw new TypeError('timeoutMs must be a positive timer duration');
  }
  return value;
}
export function createApiClient(options: ClientOptions) {
  const defaultTimeout = checkedTimeout(options.timeoutMs ?? DEFAULT_TIMEOUT_MS);
  const transport = options.fetch ?? globalThis.fetch.bind(globalThis);
  const base = options.baseUrl.replace(/\/$/, '');
  if (!base || (!base.startsWith('/') && !/^https?:\/\//.test(base)) || base.startsWith('//')) {
    throw new TypeError('baseUrl must be an HTTP URL or same-origin path');
  }
  return {
    async request<T>(path: string, decode: Decoder<T>, request: RequestOptions = {}): Promise<T> {
      if (!path.startsWith('/') || path.startsWith('//') || /[?#]/.test(base)) {
        throw new TypeError('invalid API path or baseUrl');
      }
      const timeoutMs = checkedTimeout(request.timeoutMs ?? defaultTimeout);
      const controller = new AbortController();
      let reason: 'timeout' | 'cancelled' | undefined;
      const abort = (kind: 'timeout' | 'cancelled') => {
        if (!controller.signal.aborted) {
          reason = kind;
          controller.abort();
        }
      };
      const onCancel = () => abort('cancelled');
      request.signal?.addEventListener('abort', onCancel, { once: true });
      if (request.signal?.aborted) onCancel();
      const timer = setTimeout(() => abort('timeout'), timeoutMs);
      let status: number | undefined;
      let traceId: string | undefined;
      try {
        if (reason) throw new ApiError(reason);
        const headers = new Headers({ Accept: 'application/json' });
        const init: RequestInit = { method: request.method ?? 'GET', headers, signal: controller.signal };
        if (request.body !== undefined) {
          headers.set('Content-Type', 'application/json');
          init.body = JSON.stringify(request.body);
        }
        const response = await transport(`${base}${path}`, init);
        status = response.status;
        traceId = response.headers.get('X-Trace-Id') ?? undefined;
        let payload: unknown;
        try {
          payload = await response.json();
        } catch (cause) {
          if (reason) throw new ApiError(reason, status, undefined, traceId, { cause });
          throw new ApiError(response.ok ? 'protocol' : 'http', status, undefined, traceId, { cause });
        }
        if (reason) throw new ApiError(reason, status, undefined, traceId);
        if (!isRecord(payload) || typeof payload.code !== 'string' || typeof payload.message !== 'string' ||
            typeof payload.traceId !== 'string' || !payload.traceId) {
          throw new ApiError(response.ok ? 'protocol' : 'http', status, undefined, traceId);
        }
        traceId = payload.traceId;
        if (payload.code !== 'OK') {
          if ('data' in payload) throw new ApiError('protocol', status, undefined, traceId);
          throw new ApiError('business', status, String(payload.code), traceId);
        }
        if (!response.ok || !('data' in payload)) throw new ApiError('protocol', status, undefined, traceId);
        try {
          return decode(payload);
        } catch (cause) {
          throw new ApiError('protocol', status, undefined, traceId, { cause });
        }
      } catch (cause) {
        if (cause instanceof ApiError) throw cause;
        throw new ApiError(reason ?? 'network', status, undefined, traceId, { cause });
      } finally {
        clearTimeout(timer);
        request.signal?.removeEventListener('abort', onCancel);
      }
    },
  };
}
export type ApiClient = ReturnType<typeof createApiClient>;

// Handwritten boundary validation; platform.api remains the type source.
export function decodeHealth(value: unknown): HealthResponse {
  if (!isRecord(value) || value.code !== 'OK' || typeof value.message !== 'string' ||
      typeof value.traceId !== 'string' || !value.traceId || !isRecord(value.data) ||
      (value.data.status !== 'live' && value.data.status !== 'ready')) {
    throw new TypeError('Invalid health response');
  }
  return { code: value.code, message: value.message, traceId: value.traceId, data: { status: value.data.status } };
}
