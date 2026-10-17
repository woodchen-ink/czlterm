"use client";

import { useEffect, useSyncExternalStore } from "react";
import { api } from "@/lib/api";
import { getLanguagePreference, getLocale, initializeLanguage, LANGUAGE_KEY, normalizeLanguage, setLanguage, subscribeLanguage } from "@/lib/i18n";

/** 订阅偏好，组件保留表单与导航状态，仅更新可见文案。 */
export function useLanguage() {
  useSyncExternalStore(subscribeLanguage, () => `${getLanguagePreference()}:${getLocale()}`, () => "system:zh-CN");
  return getLanguagePreference();
}

/** 在根布局只恢复一次偏好，并跟随系统和同源窗口的语言变化。 */
export function LanguageProvider({ children }: { children: React.ReactNode }) {
  useEffect(() => {
    const syncNative = () => {
      // 浏览器预览没有 Wails 运行时；桌面端同步失败时保留可用的界面并记录原因。
      if ("go" in window) void api.SetUILanguage(getLocale()).catch((error: unknown) => console.warn("Native language update failed", error));
    };
    initializeLanguage();
    syncNative();
    const unsubscribe = subscribeLanguage(syncNative);
    const onSystemChange = () => setLanguage(getLanguagePreference(), false);
    const onStorage = (event: StorageEvent) => {
      if (event.key === LANGUAGE_KEY || event.key === null) setLanguage(normalizeLanguage(event.newValue), false);
    };
    window.addEventListener("languagechange", onSystemChange);
    window.addEventListener("storage", onStorage);
    return () => {
      unsubscribe();
      window.removeEventListener("languagechange", onSystemChange);
      window.removeEventListener("storage", onStorage);
    };
  }, []);
  return children;
}
