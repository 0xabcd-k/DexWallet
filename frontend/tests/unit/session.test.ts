import { beforeEach, describe, expect, it } from 'vitest';
import { createSessionStore, readCredential, SESSION_KEY } from '../../apps/user/src/auth/session';
const idx = '01234567890123456';
function token(exp: number, version = 1, sub = idx) {
  const claims = { iss: 'dexwallet_test', aud: ['product-user'], sub, auth_version: version, exp, iat: exp - 259200 };
  return `e30.${btoa(JSON.stringify(claims))}.test-signature`;
}
const lock = async <T,>(work: () => T) => work();
beforeEach(() => localStorage.clear());
describe('product credential lifecycle', () => {
  it('a newer authentication attempt invalidates an older in-flight login', async () => {
    const store = createSessionStore(localStorage, lock);
    const first = await store.begin(store.capture()); const next = await store.begin(store.capture());
    expect(await store.establish(first!, token(300000))).toBe(false);
    expect(await store.establish(next!, token(300000, 1, '02234567890123456'))).toBe(true);
    expect(store.capture().credential?.idx).toBe('02234567890123456');
  });
  it('keeps the latest expiry and refuses another user, issuer or version', async () => {
    const store = createSessionStore(localStorage, lock);
    await store.establish(store.capture(), token(300000)); const ticket = store.capture();
    await store.renew(ticket, token(400000)); await store.renew(ticket, token(350000));
    await store.renew(ticket, token(500000, 2)); await store.renew(ticket, token(500000, 1, '02234567890123456'));
    await store.renew(ticket, 'malformed');
    expect(store.capture().credential?.expiresAt).toBe(400000);
    expect(store.capture().credential?.version).toBe(1);
  });
  it('cannot resurrect a logged-out user from a late login or renewal', async () => {
    const store = createSessionStore(localStorage, lock); const login = store.capture();
    await store.clear(); expect(await store.establish(login, token(300000))).toBe(false);
    await store.establish(store.capture(), token(300000)); const old = store.capture();
    await store.clear(); await store.renew(old, token(500000));
    expect(store.capture().credential).toBeNull();
  });
  it('starts a new generation on binding and ignores older reads/401/logout completions', async () => {
    const store = createSessionStore(localStorage, lock);
    await store.establish(store.capture(), token(300000)); const old = store.capture();
    await store.establish(old, token(400000, 2)); await store.renew(old, token(500000)); await store.clear(old);
    expect(store.matches(old)).toBe(false); expect(store.capture().credential?.version).toBe(2);
    const beforeRenewal = store.capture(); await store.renew(beforeRenewal, token(500000, 2));
    await store.clear(beforeRenewal, true); expect(store.capture().credential?.expiresAt).toBe(500000);
    await store.clear(beforeRenewal); expect(store.capture().credential).toBeNull();
  });
  it('persists across reloads and notices another tab before applying a response', async () => {
    const first = createSessionStore(localStorage, lock);
    await first.establish(first.capture(), token(300000));
    const second = createSessionStore(localStorage, lock); const stale = second.capture();
    expect(stale.credential?.raw).toBe(first.capture().credential?.raw);
    await first.clear(); await second.renew(stale, token(500000));
    expect(second.capture().credential).toBeNull();
    expect(JSON.parse(localStorage.getItem(SESSION_KEY)!).token).toBeNull();
  });
  it('uses memory when storage cannot be read or written and rejects corrupt storage', async () => {
    const broken = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); } };
    const store = createSessionStore(broken, async () => { throw new Error('lock unavailable too'); });
    await store.establish(store.capture(), token(300000));
    expect(store.capture().persistent).toBe(false); expect(store.capture().credential?.idx).toBe(idx);
    await store.clear(); expect(store.capture().credential).toBeNull();
    localStorage.setItem(SESSION_KEY, '{'); expect(createSessionStore(localStorage, lock).capture().credential).toBeNull();
  });
  it('does not interpret a token for another realm as a product credential', () => {
    expect(() => readCredential(`e30.${btoa(JSON.stringify({ aud: ['admin'] }))}.test`)).toThrow();
  });
});
