import { createSessionRequest } from '@dexwallet/session';
import { ApiError, isRecord, decodeData, decodeItems, type Decoder, type RequestOptions } from '@dexwallet/api-client';
import type { ChallengeData, CurrentUserData, IdentitySummary, TokenData, EmailLoginRequest, IdentityEmailRequest,
  IdentityTelegramRequest, TelegramLoginRequest, UpdateProfileRequest, SendEmailRequest, IdentityEmailCodeRequest, UnbindRequest } from '@dexwallet/api-contract/platform';
import { api } from './request';
import { readCredential, session, type Session } from '../auth/session';

function data(value: unknown): Record<string, unknown> {
  if (!isRecord(value) || !isRecord(value.data)) throw new TypeError('Missing data');
  return value.data;
}
export const decodeUser: Decoder<CurrentUserData> = decodeData;
export const decodeIdentities = decodeItems<IdentitySummary>('identities');
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
export const userRequest = createSessionRequest(api, session);
export const getCurrentUser = (signal?: AbortSignal) => userRequest('/v1/user/current', decodeUser, signal ? { signal } : {});
export const getIdentities = (signal?: AbortSignal) => userRequest('/v1/user/identities', decodeIdentities, signal ? { signal } : {});
export const updateProfile = (nickname: string) => userRequest('/v1/user/profile', decodeUser, { method: 'PATCH', body: { nickname } satisfies UpdateProfileRequest });
export const sendEmail = (input: SendEmailRequest | IdentityEmailCodeRequest) => 'purpose' in input
  ? userRequest('/v1/user/identities/email/code', decodeChallenge, { method: 'POST', body: input })
  : api.request('/v1/user/auth/email/code', decodeChallenge, { method: 'POST', body: input });
// Each submission captures its own session generation; successful proofs cannot be replayed.
export async function authenticate(kind: 'email' | 'telegram', input: EmailLoginRequest | TelegramLoginRequest | IdentityEmailRequest | IdentityTelegramRequest, ticket: Session) {
  const binding = 'purpose' in input;
  const path = binding ? `/v1/user/identities/${kind}/verify` : `/v1/user/auth/${kind}/login`;
  const options: RequestOptions = { method: 'POST', body: input };
  // Explicit identity results are adopted as a new generation, never ordinary renewal.
  const result = await api.request(path, decodeToken, binding && ticket.credential
    ? { ...options, headers: { Authorization: `Bearer ${ticket.credential.raw}` } } : options).catch(async error => {
    if (binding && needsLogin(error)) await session.clear(ticket, true);
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
    if (needsLogin(error)) await session.clear(ticket, true);
    throw error;
  });
  checkIdentityToken(ticket, result);
  if (!await session.establish(ticket, result.accessToken)) throw new ApiError('cancelled');
}

// A lost mutation response may have advanced auth_version. Reauthenticate to
// discover committed state rather than replaying a proof or retaining a ticket.
function needsLogin(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 ||
    ['network', 'timeout', 'protocol', 'http'].includes(error.kind) || (error.status ?? 0) >= 500);
}
