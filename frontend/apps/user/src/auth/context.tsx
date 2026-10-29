import { createContext, useContext, useEffect, useState, useSyncExternalStore, type ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router';
import { Button } from 'antd-mobile';
import { ApiError } from '@dexwallet/api-client';
import { errorTranslationKey, useTranslation } from '@dexwallet/i18n';
import type { CurrentUserData, IdentitySummary } from '@dexwallet/api-contract/platform';
import { getCurrentUser, getIdentities } from '../api/account';
import type { LaunchContext } from '../app/telegram/bootstrap';
import { session, type Session } from './session';

interface Account { generation: string; user?: CurrentUserData; identities: IdentitySummary[]; error?: unknown }
interface AuthContextValue extends Account { session: Session; loading: boolean; refresh: () => void; launch: LaunchContext }
const AuthContext = createContext<AuthContextValue | undefined>(undefined);
export function AuthProvider({ launch, children }: { launch: LaunchContext; children: ReactNode }) {
  const saved = useSyncExternalStore(session.subscribe, session.getSnapshot);
  const [revision, setRevision] = useState(0);
  const [account, setAccount] = useState<Account>({ generation: '', identities: [] });
  useEffect(() => {
    if (!saved.credential) return;
    const controller = new AbortController();
    let active = true;
    setAccount(previous => previous.user && previous.user.idx === saved.credential?.idx
      ? { generation: saved.generation, user: previous.user, identities: previous.identities }
      : { generation: saved.generation, identities: [] });
    void Promise.all([getCurrentUser(controller.signal), getIdentities(controller.signal)]).then(([user, identities]) => {
      if (active && session.matches(saved)) setAccount({ generation: saved.generation, user, identities });
    }, error => {
      if (active && session.matches(saved)) setAccount({ generation: saved.generation, identities: [], error });
    });
    return () => { active = false; controller.abort(); };
  }, [saved.generation, revision]);
  const current = saved.credential && (account.generation === saved.generation || account.user?.idx === saved.credential.idx) ? account : { generation: saved.generation, identities: [] };
  return <AuthContext.Provider value={{ ...current, session: saved, launch,
    loading: !!saved.credential && !current.user && !current.error, refresh: () => setRevision(n => n + 1) }}>{children}</AuthContext.Provider>;
}
export function useAuth() { const auth = useContext(AuthContext); if (!auth) throw new Error('Missing auth provider'); return auth; }
export function Failure({ error }: { error: unknown }) {
  const { t } = useTranslation();
  return <p role="alert" className="auth-error">{t(errorTranslationKey(error instanceof ApiError ? error : { kind: 'generic' }))}</p>;
}
const LOGOUT_LAUNCH_KEY = 'dexwallet:user:logged-out-launch';
export function autoLoginAllowed(launch: LaunchContext): boolean {
  try { return sessionStorage.getItem(LOGOUT_LAUNCH_KEY) !== launch.launchId; } catch { return false; }
}
export async function signOut(launch: LaunchContext, ticket = session.capture()) {
  try { sessionStorage.setItem(LOGOUT_LAUNCH_KEY, launch.launchId); } catch { /* Still clear the in-memory session. */ }
  await session.clear(ticket);
}
export function RequireAuth({ children }: { children: ReactNode }) {
  const auth = useAuth(); const location = useLocation(); const { t } = useTranslation();
  if (!auth.session.credential) return <Navigate to={`/login?returnTo=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  if (auth.loading) return <p role="status">{t('common:loading')}</p>;
  if (auth.error) return <section><Failure error={auth.error} /><Button onClick={auth.refresh}>{t('common:retry')}</Button>{auth.launch.mode !== 'telegram' && <Button onClick={() => { void signOut(auth.launch); }}>{t('signOut')}</Button>}</section>;
  return children;
}
