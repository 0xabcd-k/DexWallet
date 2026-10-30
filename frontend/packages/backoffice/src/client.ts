import {
  ApiError,
  createApiClient,
  isRecord,
  decodeData,
  decodeItems,
  type Decoder,
} from '@dexwallet/api-client';
import {
  createBrowserSession,
  readCredential,
  createSessionRequest,
  type Session,
} from '@dexwallet/session';
import type {
  BackofficeCodeRequest, BackofficeLoginRequest, BackofficeChangePasswordRequest,
  BackofficeAccount,
  BackofficeAuditItem,
  ManagedUserData,
  ChallengeData,
  TokenData,
} from '@dexwallet/api-contract/platform';
export type Realm = 'admin' | 'merchant';
function data(value: unknown): Record<string, unknown> {
  if (!isRecord(value) || !isRecord(value.data))
    throw new TypeError('Missing data');
  return value.data;
}
function text(row: Record<string, unknown>, key: string): string {
  const v = row[key];
  if (typeof v !== 'string') throw new TypeError(key);
  return v;
}
function number(row: Record<string, unknown>, key: string): number {
  const v = row[key];
  if (typeof v !== 'number' || !Number.isSafeInteger(v))
    throw new TypeError(key);
  return v;
}
export const decodeAccount: Decoder<BackofficeAccount> = decodeData;
export const decodeAccounts = decodeItems<BackofficeAccount>();
export const decodeUser: Decoder<ManagedUserData> = decodeData;
export const decodeAudits = decodeItems<BackofficeAuditItem>();
export function createBackofficeClient(realm: Realm, baseUrl: string) {
  const audience = `${realm}-user`;
  const session = createBrowserSession(`dexwallet:${realm}:session`, audience);
  const api = createApiClient({ baseUrl });
  const prefix = `/v1/${realm}`;
  const decodeToken: Decoder<TokenData> = (value) => {
    const row = data(value);
    const raw = text(row, 'accessToken');
    const c = readCredential(raw, audience);
    if (row.userIdx !== c.idx || row.expiresAt !== c.expiresAt)
      throw new TypeError('token');
    return { accessToken: raw, userIdx: c.idx, expiresAt: c.expiresAt };
  };
  const request = createSessionRequest(api, session, prefix);
  return {
    realm,
    session,
    request,
    sendCode(email: string, password: string, locale: BackofficeCodeRequest['locale']) {
      return api.request(
        prefix + '/auth/code',
        (v) => {
          const row = data(v);
          return {
            challenge: text(row, 'challenge'),
            expiresAt: number(row, 'expiresAt'),
          } satisfies ChallengeData;
        },
        { method: 'POST', body: { email, password, locale } satisfies BackofficeCodeRequest },
      );
    },
    async login(
      email: string,
      password: string,
      challenge: string,
      code: string,
      ticket: Session,
    ) {
      const token = await api.request(prefix + '/auth/login', decodeToken, {
        method: 'POST',
        body: { email, password, challenge, code, locale: 'en' } satisfies BackofficeLoginRequest,
      });
      if (!(await session.establish(ticket, token.accessToken)))
        throw new ApiError('cancelled');
    },
    async changePassword(oldPassword: string, password: string) {
      const ticket = session.capture();
      const token = await request(
        '/password',
        decodeToken,
        { method: 'POST', body: { oldPassword, password } satisfies BackofficeChangePasswordRequest },
        ticket,
      );
      if (!ticket.credential) throw new ApiError('cancelled');
      const next = readCredential(token.accessToken, audience);
      if (
        next.idx !== ticket.credential.idx ||
        next.issuer !== ticket.credential.issuer ||
        next.version !== ticket.credential.version + 1
      )
        throw new ApiError('protocol');
      if (!(await session.establish(ticket, token.accessToken)))
        throw new ApiError('cancelled');
    },
  };
}
export type BackofficeClient = ReturnType<typeof createBackofficeClient>;
