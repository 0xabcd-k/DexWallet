import { useState } from 'react';
import { useNavigate } from 'react-router';
import { Button, Dialog, Input } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import { logoutAll, updateProfile } from '../../api/account';
import { Failure, signOut, useAuth } from '../../auth/context';
import '../login/style.less';

export function AccountProfile() {
  const auth = useAuth(); const { t } = useTranslation();
  const [nickname, setNickname] = useState(auth.user?.nickname ?? '');
  const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>(); const [saved, setSaved] = useState(false);
  async function save() {
    setBusy(true); setError(undefined); setSaved(false);
    try { await updateProfile(nickname); setSaved(true); auth.refresh(); } catch (failure) { setError(failure); } finally { setBusy(false); }
  }
  return <div className="settings-profile">
    {auth.user?.avatarUrl && /^https?:\/\//.test(auth.user.avatarUrl) && <img className="profile-avatar" src={auth.user.avatarUrl} alt="" referrerPolicy="no-referrer" />}
    <p className="profile-name">{auth.user?.nickname || t('defaultNickname')}</p>
    <div className="settings-row"><span>{t('accountId')}</span><code>{auth.user?.idx}</code></div>
    <form className="auth-form" onSubmit={event => { event.preventDefault(); void save(); }}>
      <label htmlFor="nickname">{t('nickname')}</label><Input id="nickname" value={nickname} onChange={setNickname} maxLength={128} disabled={busy} />
      <Button type="submit" color="primary" loading={busy} disabled={busy || !nickname.trim()}>{t('saveProfile')}</Button>
    </form>{saved && <p role="status">{t('profileSaved')}</p>}
    {error !== undefined && <Failure error={error} />}
  </div>;
}

export function AccountActions() {
  const auth = useAuth(); const { t } = useTranslation(); const navigate = useNavigate();
  const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>();
  async function leave(all: boolean) {
    if (all && !await Dialog.confirm({ title: t('signOutAll'), content: t('signOutAllBody'), confirmText: t('confirm'), cancelText: t('cancel') })) return;
    setBusy(true); setError(undefined);
    try { if (all) await logoutAll(auth.session); await signOut(auth.launch, auth.session); navigate('/wallet', { replace: true }); }
    catch (failure) { setError(failure); } finally { setBusy(false); }
  }
  return <>
    {error !== undefined && <Failure error={error} />}
    <div className="settings-account-actions">
      <Button disabled={busy} onClick={() => { void signOut(auth.launch).then(() => navigate('/login?manual=1&returnTo=%2Fsettings')); }}>{t('switchAccount')}</Button>
      <Button disabled={busy} onClick={() => { void leave(false); }}>{t('signOut')}</Button>
      <Button disabled={busy} onClick={() => { void leave(true); }}>{t('signOutAll')}</Button>
    </div>{!auth.session.persistent && <p role="status" className="muted">{t('memorySession')}</p>}
  </>;
}
