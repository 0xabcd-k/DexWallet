import { describe, it, expect, vi } from 'vitest';
import { ApiError, createApiClient, decodeHealth } from '@dexwallet/api-client';
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
  it.each([new DOMException('caller deadline', 'TimeoutError'), new ApiError('business')])('treats arbitrary caller abort reasons as cancellation', async reason => {
    const { client, fetch } = setup();
    const controller = new AbortController(); controller.abort(reason);
    await expect(client.request('/health/live', decodeHealth, { signal: controller.signal })).rejects.toMatchObject({ kind: 'cancelled' });
    expect(fetch).not.toHaveBeenCalled();
  });
  it.each(['cancelled', 'timeout'] as const)('preserves the first %s when both abort sources fire before transport settles', async first => {
    const deadline = new AbortController();
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal);
    let fail: (reason: unknown) => void = () => { throw new Error('transport not called'); };
    const fetch: typeof globalThis.fetch = () => new Promise((_resolve, reject) => { fail = reject; });
    const controller = new AbortController();
    const pending = createApiClient({ baseUrl: '/api', fetch, timeoutMs: 20 }).request('/health/live', decodeHealth, { signal: controller.signal });
    const assertion = expect(pending).rejects.toMatchObject({ kind: first });
    if (first === 'cancelled') controller.abort();
    deadline.abort(new DOMException('deadline', 'TimeoutError'));
    controller.abort();
    fail(new Error('transport completed after abort'));
    await assertion;
  });
  it('keeps the deadline active while reading the response body', async () => {
    const deadline = new AbortController();
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal);
    const fetch: typeof globalThis.fetch = async (_url, init) => new Response(new ReadableStream({
      start(stream) { init?.signal?.addEventListener('abort', () => stream.error(new Error('aborted')), { once: true }); },
    }));
    const pending = createApiClient({ baseUrl: '/api', fetch }).request('/health/live', decodeHealth);
    const assertion = expect(pending).rejects.toMatchObject({ kind: 'timeout', status: 200 });
    deadline.abort(new DOMException('deadline', 'TimeoutError'));
    await assertion;
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

it('supplies headers per request and observes renewal only after a valid success', async () => {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(new Response(JSON.stringify(good), { headers: { 'X-Access-Token': 'new-token' } }));
  const client = createApiClient({ baseUrl: '/api', fetch }); const observed = vi.fn();
  await client.request('/current', decodeHealth, { headers: { Authorization: 'Bearer private-token' }, onSuccess: headers => observed(headers.get('X-Access-Token')) });
  expect(new Headers(fetch.mock.calls[0]?.[1]?.headers).get('Authorization')).toBe('Bearer private-token');
  expect(observed).toHaveBeenCalledWith('new-token');
  observed.mockClear(); fetch.mockResolvedValue(new Response(JSON.stringify({ ...good, data: {} }), { headers: { 'X-Access-Token': 'must-not-adopt' } }));
  await expect(client.request('/current', decodeHealth, { onSuccess: observed })).rejects.toMatchObject({ kind: 'protocol' });
  expect(observed).not.toHaveBeenCalled();
  expect(new Headers(fetch.mock.calls[1]?.[1]?.headers).has('Authorization')).toBe(false);
});
