import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { updateMe } from "../api";
import { setLanguage, SUPPORTED_LANGUAGES, type UiLanguage } from "../i18n";
import { menuIconClass, menuItemClass } from "../ThreadActionsMenu";
import { Icon } from "./Icon";

/**
 * UserMenu — popup opened from the sidebar user row. Settings opens the settings
 * modal; Language expands an inline English/Deutsch picker that switches the UI
 * locale and persists it to the profile (coupled with the LLM answer language);
 * Log out runs the existing logout. Styling mirrors ThreadActionsMenu.
 */
export function UserMenu({
  onSettings,
  onLogout,
  onClose,
  className = "bottom-full left-0 mb-2",
}: {
  onSettings(): void;
  onLogout(): void;
  onClose(): void;
  className?: string;
}) {
  const { t, i18n } = useTranslation();
  const [showLanguages, setShowLanguages] = useState(false);

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const active = (i18n.language.startsWith("de") ? "de" : "en") as UiLanguage;

  function chooseLanguage(language: UiLanguage) {
    const previous = active;
    setLanguage(language);
    setShowLanguages(false);
    onClose();
    // Persist to the profile so the choice survives reloads and drives the LLM
    // answer language. Revert the UI on failure so the two stay consistent.
    updateMe({ responseLanguage: language }).catch(() => setLanguage(previous));
  }

  const languageLabel: Record<UiLanguage, string> = {
    en: t("language.english"),
    de: t("language.german"),
  };

  return (
    <div
      aria-label={t("userMenu.label")}
      className={`ui-sidebar-text absolute z-30 w-[220px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => {
          onClose();
          onSettings();
        }}
      >
        <Icon name="settings" size="19px" className={menuIconClass} />
        {t("userMenu.settings")}
      </button>
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        aria-expanded={showLanguages}
        onClick={() => setShowLanguages((open) => !open)}
      >
        <Icon name="globe" size="19px" className={menuIconClass} />
        {t("userMenu.language")}
      </button>
      {showLanguages &&
        SUPPORTED_LANGUAGES.map((language) => (
          <button
            key={language}
            className={`${menuItemClass} pl-[42px] text-[#f3f0e8]`}
            role="menuitemradio"
            aria-checked={active === language}
            type="button"
            onClick={() => chooseLanguage(language)}
          >
            <span className={menuIconClass}>
              {active === language && <Icon name="check" size="17px" className="text-[#5599e7]" />}
            </span>
            {languageLabel[language]}
          </button>
        ))}
      <div className="mx-[14px] my-[5px] h-px bg-[#4a4741]" role="separator" />
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => {
          onClose();
          onLogout();
        }}
      >
        <LogoutMenuIcon />
        {t("userMenu.logout")}
      </button>
    </div>
  );
}

function LogoutMenuIcon() {
  return (
    <svg className={menuIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path
        d="M14 7V5.5C14 4.7 13.3 4 12.5 4H6C5.2 4 4.5 4.7 4.5 5.5v13c0 .8.7 1.5 1.5 1.5h6.5c.8 0 1.5-.7 1.5-1.5V17"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path d="M10 12h10m0 0-3-3m3 3-3 3" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
