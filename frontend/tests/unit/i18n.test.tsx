import { it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createAppI18n, I18nextProvider, LanguageControl, selectLanguage, normalizeLanguage, languageKey, errorTranslationKey, formatAmount, formatTime } from '@dexwallet/i18n';
it('uses preference, Telegram, supported browser and English fallback in order', () => {
  expect(selectLanguage('en-GB', 'zh-Hans', ['zh-CN'])).toBe('en');
  expect(selectLanguage(undefined, 'zh-Hans', ['en'])).toBe('zh-CN');
  expect(selectLanguage(undefined, 'zh-Hant', ['fr', 'zh-SG'])).toBe('zh-CN');
  expect(selectLanguage(undefined, undefined, ['zh', 'fr'])).toBe('en');
  expect(normalizeLanguage('not_a_locale')).toBeUndefined();
  expect(normalizeLanguage('zh-Hant-CN')).toBeUndefined();
});
it('switches language without resetting input and keeps preferences separate', async () => {
  const user = userEvent.setup();
  const i18n = await createAppI18n('user', { en: {}, 'zh-CN': {} }, 'en');
  render(<I18nextProvider i18n={i18n}><LanguageControl app="user" /><input aria-label="draft" defaultValue="keep this" /></I18nextProvider>);
  await user.selectOptions(screen.getByRole('combobox'), 'zh-CN');
  expect(document.documentElement.lang).toBe('zh-CN');
  expect(screen.getByLabelText('draft')).toHaveValue('keep this');
  expect(localStorage.getItem(languageKey('user'))).toBe('zh-CN');
  expect(localStorage.getItem(languageKey('merchant'))).toBeNull();
  expect(screen.getByText('语言')).toBeInTheDocument();
});
it('uses a generic translation for unknown future errors', () => {
  expect(errorTranslationKey({ kind: 'business', code: 'FUTURE' })).toBe('errors:generic');
  expect(errorTranslationKey({ kind: 'timeout' })).toBe('errors:timeout');
});
it('keeps decimal precision, units, negative values and unknown separate from zero', () => {
  expect(formatAmount('12345678901234567890.12345678', 'USDT')).toBe('12,345,678,901,234,567,890.12345678 USDT');
  expect(formatAmount('-1234.00', 'USD')).toBe('-1,234.00 USD');
  expect(formatAmount(null, 'USD')).toBeNull();
  expect(formatAmount('0', 'USD')).toBe('0 USD');
  expect(() => formatAmount('1e9', 'USD')).toThrow();
});
it('formats timestamps with explicit timezone and rejects invalid input', () => {
  expect(formatTime(0, 'en', 'Asia/Shanghai')).toContain('GMT+8');
  expect(formatTime(0, 'en', 'UTC')).toContain('1970');
  expect(() => formatTime(NaN, 'en')).toThrow();
});
it('has matching language keys and interpolation parameters in every namespace', async () => {
  const { common, errors } = await import('@dexwallet/i18n');
  const apps = await Promise.all([
    import('../../apps/user/src/locales'), import('../../apps/merchant/src/locales'), import('../../apps/admin/src/locales'),
  ]);
  for (const resources of [common, errors, ...apps.map(app => app.appResources)]) {
    expect(Object.keys(resources.en).sort()).toEqual(Object.keys(resources['zh-CN']).sort());
    for (const key of Object.keys(resources.en)) {
      const en = (resources.en as Record<string, string>)[key]!;
      const zh = (resources['zh-CN'] as Record<string, string>)[key]!;
      expect(en.match(/{{[^}]+}}/g) ?? []).toEqual(zh.match(/{{[^}]+}}/g) ?? []);
    }
  }
});
