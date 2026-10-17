import { english } from "@/lib/i18n-messages";

export type Locale = "zh-CN" | "en";
export type LanguagePreference = "system" | Locale;

export const LANGUAGE_KEY = "czlterm.preferences.language";
let preference: LanguagePreference = "system";
let locale: Locale = "zh-CN";
const listeners = new Set<() => void>();

/** 使用整句模板翻译，插值原样保留，避免翻译连接名或文件内容。 */
export function t(message: string, ...values: unknown[]): string {
  const template = locale === "en" ? english[message] ?? message : message;
  return template.replace(/\{(\d+)\}/g, (token, index: string) => Number(index) < values.length ? String(values[Number(index)] ?? "") : token);
}

/** 校验持久化偏好，损坏值按跟随系统处理。 */
export function normalizeLanguage(value: unknown): LanguagePreference {
  return value === "en" || value === "zh-CN" ? value : "system";
}

/** 中文系统使用中文，其他系统使用英文。 */
export function resolveLocale(value: LanguagePreference, systemLanguage: string): Locale {
  return value === "system" ? (/^zh\b/i.test(systemLanguage) ? "zh-CN" : "en") : value;
}

/** 读取当前语言，供非 React 的错误提示和格式化函数使用。 */
export function getLocale(): Locale {
  return locale;
}

/** 读取用户选择，而不是解析后的系统语言。 */
export function getLanguagePreference(): LanguagePreference {
  return preference;
}

/** 订阅语言变化，不重挂载业务组件。 */
export function subscribeLanguage(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** 应用偏好并通知界面；存储不可用时仍允许本次切换。 */
export function setLanguage(value: LanguagePreference, persist = true): void {
  preference = normalizeLanguage(value);
  locale = resolveLocale(preference, typeof navigator === "undefined" ? "zh-CN" : navigator.language);
  if (typeof document !== "undefined") document.documentElement.lang = locale;
  if (persist) {
    try { localStorage.setItem(LANGUAGE_KEY, preference); } catch { /* 存储受限时仅保留本次选择。 */ }
  }
  listeners.forEach((listener) => listener());
}

/** 挂载后恢复偏好，避免静态导出水合不一致。 */
export function initializeLanguage(): void {
  let value: unknown;
  try { value = localStorage.getItem(LANGUAGE_KEY); } catch { /* 默认跟随系统。 */ }
  setLanguage(normalizeLanguage(value), false);
}
