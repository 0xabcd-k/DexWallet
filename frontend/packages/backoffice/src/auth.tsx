import {
  createContext,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
} from 'react';
import { Navigate, Outlet, useLocation } from 'react-router';
import { Alert, Button } from 'antd';
import { useTranslation, errorTranslationKey } from '@dexwallet/i18n';
import { ApiError } from '@dexwallet/api-client';
import type { BackofficeAccount } from '@dexwallet/api-contract/platform';
import type { Session } from '@dexwallet/session';
import { decodeAccount, type BackofficeClient } from './client';
interface Auth {
  client: BackofficeClient;
  session: Session;
  account?: BackofficeAccount;
  loading: boolean;
  error?: unknown;
  refresh: () => void;
}
const AuthContext = createContext<Auth | undefined>(undefined);

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error('Missing backoffice provider');
  return value;
}
export function Failure({ error }: { error: unknown }) {
  const { t } = useTranslation();
  const key =
    error instanceof ApiError && error.code === 'AUTH_PROOF_INVALID'
      ? 'boProofInvalid'
      : errorTranslationKey(
          error instanceof ApiError ? error : { kind: 'generic' },
        );
  return <Alert type="error" role="alert" title={t(key)} />;
}
export function Provider({ client }: { client: BackofficeClient }) {
  const saved = useSyncExternalStore(
    client.session.subscribe,
    client.session.getSnapshot,
  );
  const [state, setState] = useState<{
    generation: string;
    account?: BackofficeAccount;
    error?: unknown;
  }>({ generation: '' });
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!saved.credential) return;
    const controller = new AbortController();
    let active = true;
    setState((previous) => ({
      generation: saved.generation,
      ...(previous.account?.idx === saved.credential?.idx
        ? { account: previous.account }
        : {}),
    }));
    void client
      .request('/current', decodeAccount, { signal: controller.signal }, saved)
      .then(
        (account) => {
          if (active && client.session.matches(saved))
            setState({ generation: saved.generation, account });
        },
        (error) => {
          if (active && client.session.matches(saved))
            setState({ generation: saved.generation, error });
        },
      );
    return () => {
      active = false;
      controller.abort();
    };
  }, [client, saved.generation, revision]);
  const current =
    saved.credential &&
    (state.generation === saved.generation ||
      state.account?.idx === saved.credential.idx)
      ? state
      : { generation: saved.generation };
  return (
    <AuthContext.Provider
      value={{
        client,
        session: saved,
        ...(current.account ? { account: current.account } : {}),
        loading: !!saved.credential && !current.account && !current.error,
        error: current.error,
        refresh: () => setRevision((n) => n + 1),
      }}
    >
      <Outlet />
    </AuthContext.Provider>
  );
}
export function Guard() {
  const auth = useAuth();
  const location = useLocation();
  const { t } = useTranslation();
  if (!auth.session.credential)
    return (
      <Navigate
        to={`/login?returnTo=${encodeURIComponent(location.pathname + location.search)}`}
        replace
      />
    );
  if (auth.loading) return <p role="status">{t('common:loading')}</p>;
  if (auth.error)
    return (
      <section className="desktop-page">
        <Failure error={auth.error} />
        <Button onClick={auth.refresh}>{t('common:retry')}</Button>
        <Button
          onClick={() => {
            void auth.client.session.clear();
          }}
        >
          {t('boSignOut')}
        </Button>
      </section>
    );
  if (auth.account?.mustChangePassword && location.pathname !== '/settings')
    return <Navigate to="/settings" replace />;
  return <Outlet />;
}
