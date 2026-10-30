import {
  ApiError,
  createApiClient,
  isRecord,
  type Decoder,
  type RequestOptions,
} from '@dexwallet/api-client';
import {
  createBrowserSession,
  readCredential,
  TOKEN_HEADER,
  type Session,
} from '@dexwallet/session';
import type {
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
function status(row: Record<string, unknown>): 'active' | 'disabled' {
  const v = row.status;
  if (v !== 'active' && v !== 'disabled') throw new TypeError('status');
  return v;
}
function account(row: Record<string, unknown>): BackofficeAccount {
  if (typeof row.mustChangePassword !== 'boolean')
    throw new TypeError('password requirement');
  return {
    idx: text(row, 'idx'),
    email: text(row, 'email'),
    status: status(row),
    mustChangePassword: row.mustChangePassword,
  };
}
export const decodeAccount: Decoder<BackofficeAccount> = (v) =>
  account(data(v));
export const decodeAccounts: Decoder<BackofficeAccount[]> = (v) => {
  const rows = data(v).items;
  if (!Array.isArray(rows)) throw new TypeError('items');
  return rows.map((row) => {
    if (!isRecord(row)) throw new TypeError('account');
    return account(row);
  });
};
export const decodeUser: Decoder<ManagedUserData> = (v) => {
  const row = data(v);
  return {
    idx: text(row, 'idx'),
    nickname: text(row, 'nickname'),
    status: status(row),
  };
};
export const decodeAudits: Decoder<BackofficeAuditItem[]> = (v) => {
  const rows = data(v).items;
  if (!Array.isArray(rows)) throw new TypeError('items');
  return rows.map((row) => {
    if (!isRecord(row)) throw new TypeError('audit');
    return {
      id: number(row, 'id'),
      actorRealm: text(row, 'actorRealm'),
      actorIdx: text(row, 'actorIdx'),
      action: text(row, 'action'),
      targetRealm: text(row, 'targetRealm'),
      targetIdx: text(row, 'targetIdx'),
      targetEmail: text(row, 'targetEmail'),
      beforeValue: text(row, 'beforeValue'),
      afterValue: text(row, 'afterValue'),
      reason: text(row, 'reason'),
      result: text(row, 'result'),
      traceId: text(row, 'traceId'),
      createdAt: number(row, 'createdAt'),
    };
  });
};
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
  async function request<T>(
    path: string,
    decode: Decoder<T>,
    options: RequestOptions = {},
    ticket = session.capture(),
  ): Promise<T> {
    if (!ticket.credential)
      throw new ApiError('business', 401, 'UNAUTHENTICATED');
    try {
      const result = await api.request(prefix + path, decode, {
        ...options,
        headers: { Authorization: `Bearer ${ticket.credential.raw}` },
        onSuccess: async (h) => {
          const raw = h.get(TOKEN_HEADER);
          if (raw) await session.renew(ticket, raw);
        },
      });
      if (!session.matches(ticket)) throw new ApiError('cancelled');
      return result;
    } catch (error) {
      if (error instanceof ApiError && error.status === 401)
        await session.clear(ticket, true);
      throw error;
    }
  }
  return {
    realm,
    session,
    request,
    sendCode(email: string, password: string, locale: string) {
      return api.request(
        prefix + '/auth/code',
        (v) => {
          const row = data(v);
          return {
            challenge: text(row, 'challenge'),
            expiresAt: number(row, 'expiresAt'),
          } satisfies ChallengeData;
        },
        { method: 'POST', body: { email, password, locale } },
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
        body: { email, password, challenge, code },
      });
      if (!(await session.establish(ticket, token.accessToken)))
        throw new ApiError('cancelled');
    },
    async changePassword(oldPassword: string, password: string) {
      const ticket = session.capture();
      const token = await request(
        '/password',
        decodeToken,
        { method: 'POST', body: { oldPassword, password } },
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
