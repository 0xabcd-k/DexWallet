import { createInstance } from 'i18next';
export { I18nextProvider, useTranslation } from 'react-i18next';
export { LanguageControl } from './language-control';
export { formatAmount, formatTime } from './format';

export const SUPPORTED_LANGUAGES = ['zh-CN', 'en'] as const;
export type Language = typeof SUPPORTED_LANGUAGES[number];
export type AppName = 'user' | 'merchant' | 'admin';
export function languageKey(app: AppName): string { return `dexwallet:${app}:language`; }
export function normalizeLanguage(input: string | undefined): Language | undefined {
  if (!input) return;
  try {
    const locale = new Intl.Locale(input);
    if (locale.language === 'en') return 'en';
    if (locale.language !== 'zh') return;
    if (locale.script) return locale.script === 'Hans' ? 'zh-CN' : undefined;
    if (locale.region === 'CN' || locale.region === 'SG') return 'zh-CN';
  } catch { /* Unsupported or malformed language candidate. */ }
}
export function selectLanguage(preferred?: string, telegram?: string, browser: readonly string[] = []): Language {
  for (const candidate of [preferred, telegram, ...browser]) {
    const matched = normalizeLanguage(candidate);
    if (matched) return matched;
  }
  return 'en';
}
export const common = {
  en: {
    language: 'Language', languageZh: '简体中文', languageEn: 'English', back: 'Back', home: 'Home', settings: 'Settings',
    unavailable: 'Coming soon', unavailableBody: 'This feature is not available yet.',
    loading: 'Loading…', retry: 'Try again', empty: 'No items yet', noMatches: 'No matching items',
    notFound: 'Page not found', notFoundBody: 'Use the navigation to return to an available page.',
    pageError: 'Unable to display this page', unknown: 'Unavailable',
    startupError: 'Unable to restore this Telegram session. Reopen the app from Telegram.',
    devTitle: 'Development checks', request: 'Check API connection', cancel: 'Cancel request',
    connected: 'API connection available', draft: 'Sample input', details: 'Interaction sample',
    time: 'Time', amount: 'Amount', filter: 'Filter', page: 'Page',
  },
  'zh-CN': {
    language: '语言', languageZh: '简体中文', languageEn: 'English', back: '返回', home: '首页', settings: '设置',
    unavailable: '暂未开放', unavailableBody: '此功能将在后续版本开放。',
    loading: '加载中…', retry: '重试', empty: '暂无内容', noMatches: '没有符合条件的内容',
    notFound: '页面不存在', notFoundBody: '请通过导航返回可用页面。',
    pageError: '暂时无法显示此页面', unknown: '暂不可用',
    startupError: '无法恢复 Telegram 启动信息，请从 Telegram 重新打开应用。',
    devTitle: '开发检查', request: '检查 API 连接', cancel: '取消请求',
    connected: 'API 连接正常', draft: '示例输入', details: '交互样例',
    time: '时间', amount: '金额', filter: '筛选', page: '页码',
  },
};
export const errors = {
  en: {
    generic: 'Something went wrong. Please try again.', network: 'Unable to connect. Check your connection.',
    timeout: 'The request timed out. Its result may still be pending.', protocol: 'The server returned an unexpected response.',
    http: 'The service is temporarily unavailable.', cancelled: 'Request cancelled.',
    INVALID_ARGUMENT: 'Check the information you entered.', UNAUTHENTICATED: 'Your session has expired.',
    PERMISSION_DENIED: 'You do not have access to this action.', NOT_FOUND: 'The requested item was not found.',
    METHOD_NOT_ALLOWED: 'This action is not supported.', CONFLICT: 'The item has changed. Refresh and check its status.',
    UNPROCESSABLE: 'This action cannot be completed in the current state.', RATE_LIMITED: 'Too many requests. Try again later.',
    INTERNAL_ERROR: 'Something went wrong. Please try again.', SERVICE_UNAVAILABLE: 'The service is temporarily unavailable.',
    REQUEST_TIMEOUT: 'The request timed out. Its result may still be pending.', PAYLOAD_TOO_LARGE: 'The submitted content is too large.',
    AUTH_PROOF_INVALID: 'The verification code or Telegram authorization is invalid or expired.',
    IDENTITY_OCCUPIED: 'This identity is linked to another account.',
    ANOTHER_IDENTITY_REQUIRED: 'Link another sign-in method first.',
    IDENTITY_ALREADY_BOUND: 'This sign-in method is already linked. Use the replacement option.',
  },
  'zh-CN': {
    generic: '操作未完成，请稍后重试。', network: '无法连接，请检查网络。',
    timeout: '请求超时，处理结果可能仍在确认中。', protocol: '服务返回的数据不符合预期。',
    http: '服务暂时不可用。', cancelled: '请求已取消。',
    INVALID_ARGUMENT: '请检查填写的信息。', UNAUTHENTICATED: '登录状态已失效。',
    PERMISSION_DENIED: '你没有此操作的权限。', NOT_FOUND: '未找到所请求的内容。',
    METHOD_NOT_ALLOWED: '暂不支持此操作。', CONFLICT: '内容状态已变化，请刷新后确认。',
    UNPROCESSABLE: '当前状态无法执行此操作。', RATE_LIMITED: '请求过于频繁，请稍后再试。',
    INTERNAL_ERROR: '操作未完成，请稍后重试。', SERVICE_UNAVAILABLE: '服务暂时不可用。',
    REQUEST_TIMEOUT: '请求超时，处理结果可能仍在确认中。', PAYLOAD_TOO_LARGE: '提交内容过大。',
    AUTH_PROOF_INVALID: '验证码或 Telegram 授权无效或已过期。',
    IDENTITY_OCCUPIED: '该身份已绑定其他账户。',
    ANOTHER_IDENTITY_REQUIRED: '请先绑定另一种登录方式。',
    IDENTITY_ALREADY_BOUND: '已绑定该登录方式，请使用换绑功能。',
  },
};
export function errorTranslationKey(error: { kind: string; code?: string | undefined }): string {
  const key = error.code ?? error.kind;
  return Object.hasOwn(errors.en, key) ? `errors:${key}` : 'errors:generic';
}
export async function createAppI18n(app: AppName, appResources: Record<Language, Record<string, string>>, telegramLanguage?: string) {
  let preferred: string | undefined;
  try { preferred = localStorage.getItem(languageKey(app)) ?? undefined; } catch { /* Memory-only mode. */ }
  const instance = createInstance();
  await instance.init({
    lng: selectLanguage(preferred, telegramLanguage, navigator.languages),
    fallbackLng: 'en', supportedLngs: [...SUPPORTED_LANGUAGES], load: 'currentOnly',
    defaultNS: 'app', fallbackNS: 'common', interpolation: { escapeValue: false },
    resources: {
      en: { common: common.en, errors: errors.en, app: appResources.en },
      'zh-CN': { common: common['zh-CN'], errors: errors['zh-CN'], app: appResources['zh-CN'] },
    },
  });
  const updateDocument = () => { document.documentElement.lang = instance.resolvedLanguage ?? 'en'; };
  updateDocument();
  instance.on('languageChanged', updateDocument);
  return instance;
}
