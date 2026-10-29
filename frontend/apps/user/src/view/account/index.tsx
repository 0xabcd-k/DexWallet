import { useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { Button, Dialog, Input } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import { logoutAll, updateProfile } from '../../api/account';
import { Failure, signOut, useAuth } from '../../auth/context';
import '../login/style.less';

export default function AccountPage() {
  const auth = useAuth(); const { t } = useTranslation(); const navigate = useNavigate();
  const [nickname, setNickname] = useState(auth.user?.nickname ?? '');
  const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>(); const [saved, setSaved] = useState(false);
  async function save() {
    setBusy(true); setError(undefined); setSaved(false);
    try { await updateProfile(nickname); setSaved(true); auth.refresh(); } catch (failure) { setError(failure); } finally { setBusy(false); }
  }
  async function leave(all: boolean) {
    if (all && !await Dialog.confirm({ title: t('signOutAll'), content: t('signOutAllBody'), confirmText: t('confirm'), cancelText: t('cancel') })) return;
    setBusy(true); setError(undefined);
    try { if (all) await logoutAll(auth.session); await signOut(auth.launch, auth.session); navigate('/wallet', { replace: true }); }
    catch (failure) { setError(failure); } finally { setBusy(false); }
  }
  return <section className="auth-page"><h1>{t('account')}</h1>
    <div className="auth-card">
      {auth.user?.avatarUrl && /^https?:\/\//.test(auth.user.avatarUrl) && <img className="profile-avatar" src={auth.user.avatarUrl} alt="" referrerPolicy="no-referrer" />}
      <h2>{auth.user?.nickname || t('defaultNickname')}</h2><p>{t('accountId')}</p><code>{auth.user?.idx}</code>
      <form className="auth-form" onSubmit={event => { event.preventDefault(); void save(); }}>
        <label htmlFor="nickname">{t('nickname')}</label><Input id="nickname" value={nickname} onChange={setNickname} maxLength={128} disabled={busy} />
        <Button type="submit" color="primary" loading={busy} disabled={busy || !nickname.trim()}>{t('saveProfile')}</Button>
      </form>{saved && <p role="status">{t('profileSaved')}</p>}
    </div>
    <div className="auth-card"><h2>{t('identities')}</h2>
      {auth.identities.map(identity => <p key={identity.type}>{t(identity.type)} · {identity.displayName || identity.subject}</p>)}
      <Link to="/identities" state={{ from: '/account' }}>{t('manageIdentities')}</Link>
    </div>
    {error !== undefined && <Failure error={error} />}
    <div className="auth-actions"><Button disabled={busy} onClick={() => { void leave(false); }}>{t('signOut')}</Button>
      <Button disabled={busy} onClick={() => { void leave(true); }}>{t('signOutAll')}</Button>
      <Button disabled={busy} onClick={() => { void signOut(auth.launch).then(() => navigate('/login?manual=1&returnTo=%2Faccount')); }}>{t('switchAccount')}</Button>
    </div>{!auth.session.persistent && <p role="status">{t('memorySession')}</p>}
  </section>;
}
