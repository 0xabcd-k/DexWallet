import { Link } from 'react-router';
import { LanguageControl, useTranslation } from '@dexwallet/i18n';
import { RequireAuth, useAuth } from '../../auth/context';
import { AccountActions, AccountProfile } from '../account';
import IdentitySettings from '../identities';
import './style.less';

export default function Settings() {
  const { t } = useTranslation(); const auth = useAuth();
  const accountReady = auth.session.credential && auth.user && !auth.error;
  return <section className="settings-page">
    <h1>{t('common:settings')}</h1>
    <section className="settings-section"><h2>{t('account')}</h2>
      {auth.session.credential ? <RequireAuth><AccountProfile key={auth.session.credential.idx} /></RequireAuth> :
        <Link to="/login?returnTo=%2Fsettings" state={{ from: '/settings' }}>{t('signIn')}</Link>}
    </section>
    <section className="settings-section"><h2>{t('preferences')}</h2><LanguageControl app="user" /></section>
    {accountReady && <>
      <IdentitySettings />
      {auth.launch.mode !== 'telegram' && <section className="settings-section"><h2>{t('accountActions')}</h2><AccountActions /></section>}
      {!auth.session.persistent && <p role="status" className="muted">{t('memorySession')}</p>}
    </>}
  </section>;
}
