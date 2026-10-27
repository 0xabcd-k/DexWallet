import { describe, it, expect, vi } from 'vitest';
import { createApiClient, decodeHealth } from '@dexwallet/api-client';
const good = { code: 'OK', message: 'OK', traceId: 'trace-test', data: { status: 'live' } };
function setup(payload: unknown = good, status = 200) {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response(JSON.stringify(payload), { status }));
  return { fetch, client: createApiClient({ baseUrl: '/api', fetch }) };
}
describe('API boundary', () => {
  it('decodes the generated health contract with one request', async () => {
    const { fetch, client } = setup();
    await expect(client.request('/health/live', decodeHealth)).resolves.toEqual(good);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0]?.[0]).toBe('/api/health/live');
  });
  it.each([[401, 'UNAUTHENTICATED'], [403, 'PERMISSION_DENIED'], [409, 'FUTURE_CODE']] as const)('preserves HTTP %s and business code %s', async (status, code) => {
    const { client, fetch } = setup({ code, message: 'server details', traceId: 'trace-error' }, status);
    await expect(client.request('/action', decodeHealth, { method: 'POST', body: { value: '1' } })).rejects.toMatchObject({ kind: 'business', status, code, traceId: 'trace-error' });
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it.each([{}, { ...good, traceId: '' }, { ...good, data: { status: 'unexpected' } }, { ...good, code: 'ERROR' }])('rejects malformed contracts', async payload => {
    await expect(setup(payload).client.request('/health/live', decodeHealth)).rejects.toMatchObject({ kind: 'protocol' });
  });
  it('does not trust success envelopes on failed HTTP responses', async () => {
    await expect(setup(good, 503).client.request('/health/live', decodeHealth)).rejects.toMatchObject({ kind: 'protocol', status: 503 });
  });
  it.each([200, 502])('handles non-JSON status %s', async status => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response('<html>proxy</html>', { status }));
    await expect(createApiClient({ baseUrl: '/api', fetch }).request('/health/live', decodeHealth)).rejects.toMatchObject({ kind: status === 200 ? 'protocol' : 'http', status });
  });
  it('does not retry network failures', async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockRejectedValue(new TypeError('offline'));
    await expect(createApiClient({ baseUrl: '/api', fetch }).request('/action', decodeHealth, { method: 'POST' })).rejects.toMatchObject({ kind: 'network' });
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it('skips transport when already cancelled', async () => {
    const { client, fetch } = setup(); const controller = new AbortController(); controller.abort();
    await expect(client.request('/health/live', decodeHealth, { signal: controller.signal })).rejects.toMatchObject({ kind: 'cancelled' });
    expect(fetch).not.toHaveBeenCalled();
  });
  it('keeps the deadline active while reading the response body', async () => {
    vi.useFakeTimers();
    try {
      const fetch: typeof globalThis.fetch = async (_url, init) => new Response(new ReadableStream({
        start(stream) { init?.signal?.addEventListener('abort', () => stream.error(new Error('aborted')), { once: true }); },
      }));
      const pending = createApiClient({ baseUrl: '/api', fetch }).request('/health/live', decodeHealth);
      const assertion = expect(pending).rejects.toMatchObject({ kind: 'timeout', status: 200 });
      await vi.advanceTimersByTimeAsync(10_000); await assertion;
      expect(vi.getTimerCount()).toBe(0);
    } finally { vi.useRealTimers(); }
  });
  it('distinguishes caller cancellation from timeout', async () => {
    const fetch: typeof globalThis.fetch = (_url, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
    });
    const controller = new AbortController();
    const pending = createApiClient({ baseUrl: '/api', fetch }).request('/health/live', decodeHealth, { signal: controller.signal });
    controller.abort(); await expect(pending).rejects.toMatchObject({ kind: 'cancelled' });
  });
});
