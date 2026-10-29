import { ApiError, isRecord, type Decoder, type RequestOptions } from '@dexwallet/api-client';
import type { ChallengeData, CurrentUserData, IdentitySummary, TokenData, EmailLoginRequest, IdentityEmailRequest,
  IdentityTelegramRequest, TelegramLoginRequest, UpdateProfileRequest, SendEmailRequest, IdentityEmailCodeRequest, UnbindRequest } from '@dexwallet/api-contract/platform';
import { api } from './request';
import { readCredential, session, TOKEN_HEADER, type Session } from '../auth/session';

function data(value: unknown): Record<string, unknown> {
  if (!isRecord(value) || !isRecord(value.data)) throw new TypeError('Missing data');
  return value.data;
}
export const decodeUser: Decoder<CurrentUserData> = value => {
  const v = data(value);
  if (typeof v.idx !== 'string' || !/^0\d{16,19}$/.test(v.idx) || typeof v.nickname !== 'string' || typeof v.avatarUrl !== 'string') throw new TypeError('Invalid user');
  return { idx: v.idx, nickname: v.nickname, avatarUrl: v.avatarUrl };
};
export const decodeIdentities: Decoder<IdentitySummary[]> = value => {
  const v = data(value);
  if (!Array.isArray(v.identities) || v.identities.length > 2) throw new TypeError('Invalid identities');
  const result: IdentitySummary[] = [];
  for (const row of v.identities) {
    if (!isRecord(row) || (row.type !== 'email' && row.type !== 'telegram') || typeof row.subject !== 'string' || !row.subject ||
        typeof row.displayName !== 'string' || result.some(i => i.type === row.type)) throw new TypeError('Invalid identity');
    result.push({ type: row.type, subject: row.subject, displayName: row.displayName });
  }
  return result;
};
const decodeChallenge: Decoder<ChallengeData> = value => {
  const v = data(value);
  if (typeof v.challenge !== 'string' || !/^[a-f0-9]{64}$/.test(v.challenge) || typeof v.expiresAt !== 'number' || !Number.isSafeInteger(v.expiresAt)) throw new TypeError('Invalid challenge');
  return { challenge: v.challenge, expiresAt: v.expiresAt };
};
export const decodeToken: Decoder<TokenData> = value => {
  const v = data(value);
  if (typeof v.accessToken !== 'string') throw new TypeError('Invalid token response');
  const c = readCredential(v.accessToken);
  if (v.userIdx !== c.idx || v.expiresAt !== c.expiresAt) throw new TypeError('Mismatched token');
  return { accessToken: v.accessToken, userIdx: c.idx, expiresAt: c.expiresAt };
};
export async function userRequest<T>(path: string, decode: Decoder<T>, options: RequestOptions = {}, ticket = session.capture()): Promise<T> {
  if (!ticket.credential) throw new ApiError('business', 401, 'UNAUTHENTICATED');
  try {
    const result = await api.request(path, decode, { ...options, headers: { Authorization: `Bearer ${ticket.credential.raw}` },
      onSuccess: async headers => { const token = headers.get(TOKEN_HEADER); if (token) await session.renew(ticket, token); } });
    if (!session.matches(ticket)) throw new ApiError('cancelled');
    return result;
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) await session.clear(ticket, true);
    throw error;
  }
}
export const getCurrentUser = (signal?: AbortSignal) => userRequest('/v1/user/current', decodeUser, signal ? { signal } : {});
export const getIdentities = (signal?: AbortSignal) => userRequest('/v1/user/identities', decodeIdentities, signal ? { signal } : {});
export const updateProfile = (nickname: string) => userRequest('/v1/user/profile', decodeUser, { method: 'PATCH', body: { nickname } satisfies UpdateProfileRequest });
export const sendEmail = (input: SendEmailRequest | IdentityEmailCodeRequest) => 'purpose' in input
  ? userRequest('/v1/user/identities/email/code', decodeChallenge, { method: 'POST', body: input })
  : api.request('/v1/user/auth/email/code', decodeChallenge, { method: 'POST', body: input });
// Retries retain the original ticket as well as the exact material/request token.
export async function authenticate(kind: 'email' | 'telegram', input: EmailLoginRequest | TelegramLoginRequest | IdentityEmailRequest | IdentityTelegramRequest, ticket: Session) {
  const binding = 'purpose' in input;
  const path = binding ? `/v1/user/identities/${kind}/verify` : `/v1/user/auth/${kind}/login`;
  const options: RequestOptions = { method: 'POST', body: input };
  // Explicit identity results are adopted as a new generation, never ordinary renewal.
  const result = await api.request(path, decodeToken, binding && ticket.credential
    ? { ...options, headers: { Authorization: `Bearer ${ticket.credential.raw}` } } : options).catch(async error => {
    if (binding && error instanceof ApiError && error.status === 401) await session.clear(ticket, true);
    throw error;
  });
  if (binding) checkIdentityToken(ticket, result);
  if (!await session.establish(ticket, result.accessToken)) throw new ApiError('cancelled');
}
function checkIdentityToken(ticket: Session, result: TokenData) {
  const next = readCredential(result.accessToken), previous = ticket.credential;
  if (!previous || next.idx !== previous.idx || next.issuer !== previous.issuer || next.version < previous.version || next.version > previous.version + 1) throw new ApiError('protocol');
}
export async function unbind(input: UnbindRequest, ticket: Session) {
  if (!ticket.credential) throw new ApiError('business', 401, 'UNAUTHENTICATED');
  const result = await api.request('/v1/user/identities/unbind', decodeToken, { method: 'POST', body: input, headers: { Authorization: `Bearer ${ticket.credential.raw}` } }).catch(async error => {
    if (error instanceof ApiError && error.status === 401) await session.clear(ticket, true);
    throw error;
  });
  checkIdentityToken(ticket, result);
  if (!await session.establish(ticket, result.accessToken)) throw new ApiError('cancelled');
}
export async function logoutAll(ticket = session.capture()) {
  await userRequest('/v1/user/auth/logout-all', value => { if (data(value).loggedOut !== true) throw new TypeError('Invalid logout'); }, { method: 'POST' }, ticket);
  await session.clear(ticket);
}
