import {
  createSessionStore as createStore,
  createBrowserSession,
  readCredential as read,
  type SessionStorage,
  type Lock,
} from '@dexwallet/session';
export {
  TOKEN_HEADER,
  type Session,
  type Credential,
} from '@dexwallet/session';
export const SESSION_KEY = 'dexwallet:user:session';
const audience = 'product-user';
export const readCredential = (raw: string) => read(raw, audience);
export const createSessionStore = (
  storage: SessionStorage | undefined,
  lock: Lock,
) => createStore(storage, lock, SESSION_KEY, audience);
export const session = createBrowserSession(SESSION_KEY, audience);
