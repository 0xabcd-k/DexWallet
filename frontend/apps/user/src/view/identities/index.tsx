import { useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { Button, Dialog } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import type { IdentitySummary, UnbindRequest } from '@dexwallet/api-contract/platform';
import { unbind } from '../../api/account';
import { Failure, useAuth } from '../../auth/context';
import { randomRequestToken, session, type Session } from '../../auth/session';
import { safeDestination } from '../../app/navigation';
import EmailForm from '../login/email-form';
import Telegram from '../login/telegram';
import '../login/style.less';

type Editing = { type: IdentitySummary['type']; purpose: 'bind' | 'replace' };
export default function IdentitySettings() {
  const auth = useAuth(); const { t } = useTranslation(); const [params] = useSearchParams(); const navigate = useNavigate();
  const target = safeDestination(params.get('returnTo'), '/settings');
  const required = params.get('required');
  const returnToSource = params.has('returnTo') && target !== '/settings';
  const inTelegram = auth.launch.mode === 'telegram';
  const [editing, setEditing] = useState<Editing | undefined>(); const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>();
  const attempt = useRef<{ input: UnbindRequest; ticket: Session } | undefined>(undefined);
  function complete() {
    setEditing(undefined); setError(undefined); attempt.current = undefined;
    if (returnToSource) navigate(target, { replace: true });
  }
  async function remove(type: IdentitySummary['type']) {
    if (type === 'telegram' && inTelegram) return;
    if (!await Dialog.confirm({ title: t('unlinkIdentity', { identity: t(type) }), content: t('unlinkWarning'), confirmText: t('confirm'), cancelText: t('cancel') })) return;
    setBusy(true); setError(undefined);
    if (attempt.current?.input.identity !== type) attempt.current = { input: { requestToken: randomRequestToken(), identity: type }, ticket: session.capture() };
    try { await unbind(attempt.current.input, attempt.current.ticket); complete(); }
    catch (failure) { setError(failure); } finally { setBusy(false); }
  }
  return <section className="settings-section"><h2>{t('identities')}</h2><p className="muted">{t('bindingRules')}</p>
    {(required === 'email' || required === 'telegram') && !auth.identities.some(i => i.type === required) && <p role="status">{t('identityNeeded', { identity: t(required) })}</p>}
    {(['email', 'telegram'] as const).map(type => {
      const identity = auth.identities.find(i => i.type === type);
      return <div className="settings-identity" key={type}><h3>{t(type)}</h3>
        {identity ? <><p>{identity.displayName || identity.subject}</p>{identity.displayName && identity.subject !== identity.displayName && <p className="muted">{identity.subject}</p>}</> : <p>{t('notLinked')}</p>}
        <div className="auth-actions">{!(inTelegram && type === 'telegram' && identity) && <Button disabled={busy || !!editing || (!!identity && auth.identities.length < 2)} onClick={() => setEditing({ type, purpose: identity ? 'replace' : 'bind' })}>{t(identity ? 'replace' : 'link')}</Button>}
          {identity && <Button disabled={busy || !!editing || auth.identities.length < 2 || (type === 'telegram' && inTelegram)} onClick={() => { void remove(type); }}>{t('unlink')}</Button>}</div>
        {identity && type === 'telegram' && inTelegram && <p className="muted">{t('telegramUnlinkBlocked')}</p>}
        {editing?.type === type && <section className="settings-inline-form"><h4>{t(editing.purpose === 'bind' ? 'linkIdentity' : 'replaceIdentity', { identity: t(editing.type) })}</h4>
          {editing.type === 'email' ? <EmailForm purpose={editing.purpose} onSuccess={complete} /> :
            <Telegram purpose={editing.purpose} onSuccess={complete} />}
          <Button fill="none" onClick={() => setEditing(undefined)}>{t('cancel')}</Button>
        </section>}
      </div>;
    })}
    {error !== undefined && <Failure error={error} />}{returnToSource && <Link to={target} replace>{t('returnToSource')}</Link>}
  </section>;
}
