import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import { en } from "./en";
import { de } from "./de";

// Loom ships two UI languages. `de`/`en` are the only supported display locales;
// anything else (including the backend's `auto` answer-language) falls back to
// the browser locale, then English.
export type UiLanguage = "en" | "de";

const STORAGE_KEY = "loom:ui-language";

export const SUPPORTED_LANGUAGES: readonly UiLanguage[] = ["en", "de"];

function isSupported(value: string | null | undefined): value is UiLanguage {
  return value === "en" || value === "de";
}

// browserLanguage maps navigator.language ("de-CH", "de", "en-US", …) onto a
// supported UI locale. German-speaking locales pick `de`; everything else `en`.
function browserLanguage(): UiLanguage {
  const nav = (typeof navigator !== "undefined" && navigator.language) || "";
  return nav.toLowerCase().startsWith("de") ? "de" : "en";
}

// initialLanguage resolves the locale to boot with, before /api/me is known:
// the last explicit choice (localStorage) wins, else the browser locale. This
// runs synchronously at import time so there is no flash of English.
export function initialLanguage(): UiLanguage {
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(STORAGE_KEY);
  } catch {
    stored = null;
  }
  return isSupported(stored) ? stored : browserLanguage();
}

// setLanguage switches the active UI locale everywhere the DOM cares about it and
// remembers the choice for the next boot. It does NOT persist to the profile —
// callers that represent an explicit user action also PATCH /api/me.
export function setLanguage(language: UiLanguage): void {
  void i18n.changeLanguage(language);
  try {
    localStorage.setItem(STORAGE_KEY, language);
  } catch {
    // Ignore storage failures (private mode); the in-memory locale still applies.
  }
  if (typeof document !== "undefined") {
    document.documentElement.lang = language;
  }
}

// applyUserLanguage reconciles the freshly loaded profile with the UI locale.
// A pinned profile language (`en`/`de`) is authoritative; an unset/unknown value
// leaves the browser-locale choice in place (the caller seeds it — see
// seedLanguageFor).
export function applyUserLanguage(responseLanguage: string | undefined | null): void {
  if (isSupported(responseLanguage)) {
    setLanguage(responseLanguage);
  }
}

// seedLanguageFor returns the browser locale to write into a profile that has no
// supported language yet (empty/unset, or a legacy `auto`), or null when the
// profile is already pinned to `en`/`de`. The caller persists the result to the
// profile so the language stops being unset.
export function seedLanguageFor(responseLanguage: string | undefined | null): UiLanguage | null {
  return isSupported(responseLanguage) ? null : browserLanguage();
}

void i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    de: { translation: de },
  },
  lng: initialLanguage(),
  fallbackLng: "en",
  interpolation: { escapeValue: false },
});

if (typeof document !== "undefined") {
  document.documentElement.lang = i18n.language;
}

export default i18n;
