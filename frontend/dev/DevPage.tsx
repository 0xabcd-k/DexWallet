import { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useSearchParams } from 'react-router';
import { ApiError } from '@dexwallet/api-client';
import { useTranslation, LanguageControl, errorTranslationKey, formatAmount, formatTime, type AppName } from '@dexwallet/i18n';

type Health = { data: { status: 'live' | 'ready' }; traceId: string };
export function DevChecks({ app, load }: { app: AppName; load: (signal?: AbortSignal) => Promise<Health> }) {
  const { t, i18n } = useTranslation('common');
  const [draft, setDraft] = useState('');
  const [search, setSearch] = useSearchParams();
  const [state, setState] = useState<'idle' | 'loading' | 'success' | 'error'>('idle');
  const [error, setError] = useState<ApiError>();
  const [result, setResult] = useState<Health>();
  const request = useRef<AbortController>();
  const location = useLocation();
  const rawPage = Number(search.get('page') ?? 1);
  const page = Number.isSafeInteger(rawPage) && rawPage > 0 ? rawPage : 1;
  useEffect(() => () => request.current?.abort(), []);
  async function run() {
    request.current?.abort();
    const active = new AbortController(); request.current = active;
    setState('loading'); setError(undefined); setResult(undefined);
    try {
      const response = await load(active.signal);
      if (request.current === active && !active.signal.aborted) { setResult(response); setState('success'); }
    } catch (cause) {
      if (request.current !== active) return;
      const failure = cause instanceof ApiError ? cause : new ApiError('protocol');
      if (failure.kind === 'cancelled') { setState('idle'); return; }
      setError(failure); setState('error');
    }
  }
  return <section style={{ padding: 24, background: 'white', borderRadius: 16 }}>
    <h1>{t('devTitle')}</h1><LanguageControl app={app} />
    <p><label>{t('draft')} <input aria-label={t('draft')} value={draft} onChange={e => setDraft(e.target.value)} /></label></p>
    <p><label>{t('filter')} <input aria-label={t('filter')} value={search.get('filter') ?? ''} onChange={e => setSearch({ filter: e.target.value, page: '1' }, { replace: true })} /></label></p>
    <p>{t('page')}: {page}</p>
    {app === 'user' && <Link to="/__dev/details" state={{ from: location.pathname + location.search }}>{t('details')}</Link>}
    <p><button onClick={() => void run()} disabled={state === 'loading'}>{t('request')}</button>
    {state === 'loading' && <button onClick={() => { request.current?.abort(); setState('idle'); }}>{t('cancel')}</button>}</p>
    {state === 'loading' && <p role="status">{t('loading')}</p>}
    {state === 'success' && <p role="status">{t('connected')} · {result?.data.status} · {result?.traceId}</p>}
    {state === 'error' && error && <div role="alert"><p>{t(errorTranslationKey(error))}</p><p>{error.kind} · {error.status ?? '—'} · {error.code ?? '—'} · {error.traceId ?? '—'}</p><button onClick={() => void run()}>{t('retry')}</button></div>}
    <p>{t('amount')}: {formatAmount('12345678901234567890.12345678', 'USDT')}</p>
    <p>{t('amount')}: {formatAmount(null, 'USD') ?? t('unknown')}</p>
    <p>{t('time')}: {formatTime(0, i18n.resolvedLanguage === 'zh-CN' ? 'zh-CN' : 'en')}</p>
    <p>{t('empty')}</p><p>{t('noMatches')}</p>
  </section>;
}
