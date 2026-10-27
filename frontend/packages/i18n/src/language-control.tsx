import { useTranslation } from 'react-i18next';
import { languageKey, type AppName, type Language } from './index';
export function LanguageControl({ app }: { app: AppName }) {
  const { t, i18n } = useTranslation('common');
  const change = (language: Language) => {
    try { localStorage.setItem(languageKey(app), language); } catch { /* Keep the in-memory preference. */ }
    void i18n.changeLanguage(language);
  };
  return <label className="language-control">{t('language')}
    <select aria-label={t('language')} value={i18n.resolvedLanguage ?? 'en'} onChange={e => {
      if (e.target.value === 'en' || e.target.value === 'zh-CN') change(e.target.value);
    }}><option value="zh-CN">{t('languageZh')}</option><option value="en">{t('languageEn')}</option></select>
  </label>;
}
