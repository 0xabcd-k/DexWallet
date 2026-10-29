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
export default function IdentitiesPage() {
  const auth = useAuth(); const { t } = useTranslation(); const [params] = useSearchParams(); const navigate = useNavigate();
  const target = safeDestination(params.get('returnTo'), '/account');
  const required = params.get('required');
  const telegramUnlinkBlocked = auth.launch.mode === 'telegram';
  const [editing, setEditing] = useState<Editing | undefined>(); const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>();
  const attempt = useRef<{ input: UnbindRequest; ticket: Session } | undefined>(undefined);
  async function remove(type: IdentitySummary['type']) {
    if (type === 'telegram' && telegramUnlinkBlocked) return;
    if (!await Dialog.confirm({ title: t('unlinkIdentity', { identity: t(type) }), content: t('unlinkWarning'), confirmText: t('confirm'), cancelText: t('cancel') })) return;
    setBusy(true); setError(undefined);
    if (attempt.current?.input.identity !== type) attempt.current = { input: { requestToken: randomRequestToken(), identity: type }, ticket: session.capture() };
    try { await unbind(attempt.current.input, attempt.current.ticket); navigate(target, { replace: true }); }
    catch (failure) { setError(failure); } finally { setBusy(false); }
  }
  return <section className="auth-page"><h1>{t('identities')}</h1><p className="muted">{t('bindingRules')}</p>
    {(required === 'email' || required === 'telegram') && !auth.identities.some(i => i.type === required) && <p role="status">{t('identityNeeded', { identity: t(required) })}</p>}
    {(['email', 'telegram'] as const).map(type => {
      const identity = auth.identities.find(i => i.type === type);
      return <div className="auth-card" key={type}><h2>{t(type)}</h2>
        {identity ? <><p>{identity.displayName || identity.subject}</p>{identity.subject !== identity.displayName && <p className="muted">{identity.subject}</p>}</> : <p>{t('notLinked')}</p>}
        <div className="auth-actions"><Button disabled={busy || !!editing || (!!identity && auth.identities.length < 2)} onClick={() => setEditing({ type, purpose: identity ? 'replace' : 'bind' })}>{t(identity ? 'replace' : 'link')}</Button>
          {identity && <Button disabled={busy || !!editing || auth.identities.length < 2 || (type === 'telegram' && telegramUnlinkBlocked)} onClick={() => { void remove(type); }}>{t('unlink')}</Button>}</div>
        {identity && type === 'telegram' && telegramUnlinkBlocked && <p className="muted">{t('telegramUnlinkBlocked')}</p>}
      </div>;
    })}
    {editing && <section className="auth-card"><h2>{t(editing.purpose === 'bind' ? 'linkIdentity' : 'replaceIdentity', { identity: t(editing.type) })}</h2>
      {editing.type === 'email' ? <EmailForm purpose={editing.purpose} onSuccess={() => navigate(target, { replace: true })} /> :
        <Telegram purpose={editing.purpose} onSuccess={() => navigate(target, { replace: true })} />}
      <Button fill="none" onClick={() => setEditing(undefined)}>{t('cancel')}</Button>
    </section>}
    {error !== undefined && <Failure error={error} />}<Link to={target} replace>{t('returnToSource')}</Link>
  </section>;
}
