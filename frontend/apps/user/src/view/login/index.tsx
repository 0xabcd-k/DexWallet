import { Navigate, useNavigate, useSearchParams } from 'react-router';
import { LanguageControl, useTranslation } from '@dexwallet/i18n';
import { useAuth } from '../../auth/context';
import { safeDestination } from '../../app/navigation';
import EmailForm from './email-form';
import Telegram from './telegram';
import './style.less';

export default function Login() {
  const { t } = useTranslation(); const auth = useAuth(); const navigate = useNavigate(); const [params] = useSearchParams();
  const target = safeDestination(params.get('returnTo'), '/settings');
  if (auth.session.credential) return <Navigate to={target} replace />;
  return <section className="auth-page"><p className="eyebrow">DexWallet</p><h1>{t('signIn')}</h1><p className="muted">{t('signInBody')}</p>
    <div className="auth-card"><h2>{t('email')}</h2><EmailForm onSuccess={() => navigate(target, { replace: true })} /></div>
    <div className="auth-card"><h2>{t('telegram')}</h2><Telegram automatic={!params.has('manual')} onSuccess={() => navigate(target, { replace: true })} /></div>
    <LanguageControl app="user" />
    {!auth.session.persistent && <p role="status">{t('memorySession')}</p>}
  </section>;
}
