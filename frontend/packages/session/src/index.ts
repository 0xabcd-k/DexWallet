import { isRecord } from '@dexwallet/api-client';
export function randomToken(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(32)), (b) =>
    b.toString(16).padStart(2, '0'),
  ).join('');
}

export const TOKEN_HEADER = 'X-Access-Token';
const TOKEN_LIFETIME_SECONDS = 3 * 24 * 60 * 60;
export interface Credential {
  raw: string;
  idx: string;
  version: number;
  issuer: string;
  expiresAt: number;
}
export interface Session {
  generation: string;
  credential: Credential | null;
  persistent: boolean;
}
// This reads server-issued metadata for ordering only. The API independently
// verifies every signature, lifetime, user status and authentication version.
export function readCredential(raw: string, audience: string): Credential {
  if (raw.length > 4096 || raw.split('.').length !== 3)
    throw new TypeError('Invalid token');
  const part = raw.split('.')[1];
  if (!part) throw new TypeError('Missing claims');
  const bytes = Uint8Array.from(
    atob(part.replace(/-/g, '+').replace(/_/g, '/')),
    (c) => c.charCodeAt(0),
  );
  const c: unknown = JSON.parse(new TextDecoder().decode(bytes));
  if (
    !isRecord(c) ||
    typeof c.sub !== 'string' ||
    !/^0\d{16,19}$/.test(c.sub) ||
    typeof c.iss !== 'string' ||
    !c.iss ||
    !Array.isArray(c.aud) ||
    c.aud.length !== 1 ||
    c.aud[0] !== audience ||
    typeof c.auth_version !== 'number' ||
    !Number.isSafeInteger(c.auth_version) ||
    c.auth_version < 1 ||
    typeof c.iat !== 'number' ||
    !Number.isSafeInteger(c.iat) ||
    typeof c.exp !== 'number' ||
    !Number.isSafeInteger(c.exp) ||
    c.exp - c.iat !== TOKEN_LIFETIME_SECONDS
  )
    throw new TypeError('Invalid claims');
  return {
    raw,
    idx: c.sub,
    issuer: c.iss,
    version: c.auth_version,
    expiresAt: c.exp,
  };
}
export type SessionStorage = Pick<Storage, 'getItem' | 'setItem'>;
export type Lock = <T>(work: () => T) => Promise<T>;
export function createSessionStore(
  storage: SessionStorage | undefined,
  lock: Lock,
  key: string,
  audience: string,
) {
  let persistent = !!storage;
  let lastRaw: string | null = null;
  let current: Session = {
    generation: randomToken(),
    credential: null,
    persistent,
  };
  const listeners = new Set<() => void>();
  const notify = () => {
    for (const listener of listeners) listener();
  };
  function sync() {
    if (!storage) return;
    let raw: string | null;
    try {
      raw = storage.getItem(key);
    } catch {
      storage = undefined;
      persistent = false;
      current = { ...current, persistent };
      notify();
      return;
    }
    try {
      if (raw === lastRaw) return;
      lastRaw = raw;
      const saved: unknown = JSON.parse(raw ?? 'null');
      if (
        !isRecord(saved) ||
        typeof saved.generation !== 'string' ||
        (saved.token !== null && typeof saved.token !== 'string')
      )
        throw new TypeError('Invalid saved session');
      current = {
        generation: saved.generation,
        credential:
          saved.token === null ? null : readCredential(saved.token, audience),
        persistent,
      };
    } catch {
      current = { generation: randomToken(), credential: null, persistent };
    }
    notify();
  }
  function save(credential: Credential | null, generation: string) {
    const raw = JSON.stringify({ generation, token: credential?.raw ?? null });
    try {
      storage?.setItem(key, raw);
      lastRaw = raw;
    } catch {
      persistent = false;
      storage = undefined;
    }
    current = { generation, credential, persistent };
    notify();
  }
  // Memory-only sessions do not depend on a lock service tied to browser storage.
  const atomic: Lock = async (work) => (storage ? lock(work) : work());
  sync();
  return {
    getSnapshot: () => current,
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    sync,
    capture() {
      sync();
      return current;
    },
    matches(ticket: Session) {
      sync();
      return ticket.generation === current.generation;
    },
    async begin(ticket: Session) {
      return atomic(() => {
        sync();
        if (ticket.generation !== current.generation) return undefined;
        save(null, randomToken());
        return current;
      });
    },
    async establish(ticket: Session, raw: string) {
      const credential = readCredential(raw, audience);
      return atomic(() => {
        sync();
        if (ticket.generation !== current.generation) return false;
        save(credential, randomToken());
        return true;
      });
    },
    async renew(ticket: Session, raw: string) {
      let next: Credential;
      try {
        next = readCredential(raw, audience);
      } catch {
        return;
      }
      await atomic(() => {
        sync();
        const previous = current.credential;
        if (
          ticket.generation !== current.generation ||
          !previous ||
          next.idx !== previous.idx ||
          next.issuer !== previous.issuer ||
          next.version !== previous.version ||
          next.expiresAt <= previous.expiresAt
        )
          return;
        save(next, current.generation);
      });
    },
    async clear(ticket?: Session, exactToken = false) {
      await atomic(() => {
        sync();
        if (
          ticket &&
          (ticket.generation !== current.generation ||
            (exactToken && ticket.credential?.raw !== current.credential?.raw))
        )
          return;
        save(null, randomToken());
      });
    },
  };
}

export function createBrowserSession(key: string, audience: string) {
  let storage: Storage | undefined;
  try {
    storage = navigator.locks ? localStorage : undefined;
  } catch {
    /* Memory-only mode. */
  }
  const store = createSessionStore(
    storage,
    async (work) =>
      navigator.locks ? navigator.locks.request(key, work) : work(),
    key,
    audience,
  );
  window.addEventListener('storage', (event) => {
    if (event.key === key || event.key === null) store.sync();
  });
  return store;
}
