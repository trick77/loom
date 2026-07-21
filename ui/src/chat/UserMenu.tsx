import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { updateMe } from "../api";
import { setLanguage, SUPPORTED_LANGUAGES, type UiLanguage } from "../i18n";
import { menuIconClass, menuItemClass } from "../ThreadActionsMenu";
import { Icon } from "./Icon";

const LANGUAGE_FLYOUT_WIDTH = 240;

/**
 * UserMenu — popup opened from the sidebar user row. Settings opens the settings
 * modal; Language opens a side flyout (claude-style) to pick English/Deutsch,
 * which switches the UI locale and persists it to the profile (coupled with the
 * LLM answer language); Log out runs the existing logout. Styling mirrors
 * ThreadActionsMenu. The flyout is portalled to <body> so it escapes the
 * sidebar's overflow clipping, and flips to the left edge when it would overflow.
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
  const [flyoutPos, setFlyoutPos] = useState<{
    top: number;
    left: number;
  } | null>(null);
  const languageButtonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const active = (i18n.language.startsWith("de") ? "de" : "en") as UiLanguage;

  function openLanguages() {
    const rect = languageButtonRef.current?.getBoundingClientRect();
    if (rect) {
      // Prefer opening to the right of the row; flip to the left when it would
      // overflow the viewport.
      const right = rect.right + 4;
      const left =
        right + LANGUAGE_FLYOUT_WIDTH <= window.innerWidth
          ? right
          : rect.left - 4 - LANGUAGE_FLYOUT_WIDTH;
      // The account menu sits at the sidebar bottom, so clamp the flyout up when
      // it would run past the viewport bottom (row height ~34px per language).
      const estimatedHeight = SUPPORTED_LANGUAGES.length * 34 + 12;
      const top = Math.max(
        8,
        Math.min(rect.top, window.innerHeight - estimatedHeight - 8),
      );
      setFlyoutPos({ top, left });
    }
    setShowLanguages(true);
  }

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

  const panelClass =
    "ui-sidebar-text w-[240px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)]";

  return (
    <div
      aria-label={t("userMenu.label")}
      className={`absolute z-30 ${className}`}
    >
      <div className={panelClass} role="menu">
        <button
          className={`${menuItemClass} text-[#f3f0e8]`}
          role="menuitem"
          type="button"
          onMouseEnter={() => setShowLanguages(false)}
          onClick={() => {
            onClose();
            onSettings();
          }}
        >
          <Icon name="settings" size="19px" className={menuIconClass} />
          {t("userMenu.settings")}
        </button>
        <button
          ref={languageButtonRef}
          className={`${menuItemClass} text-[#f3f0e8] ${showLanguages ? "bg-[#43423d]" : ""}`}
          role="menuitem"
          type="button"
          aria-haspopup="menu"
          aria-expanded={showLanguages}
          onMouseEnter={openLanguages}
          onClick={openLanguages}
        >
          <Icon name="globe" size="19px" className={menuIconClass} />
          {t("userMenu.language")}
          <Icon
            name="chevronRight"
            size="16px"
            className="ml-auto text-[#8f8b82]"
          />
        </button>
        <div
          className="mx-[14px] my-[5px] h-px bg-[#4a4741]"
          role="separator"
        />
        <button
          className={`${menuItemClass} text-[#f3f0e8]`}
          role="menuitem"
          type="button"
          onMouseEnter={() => setShowLanguages(false)}
          onClick={() => {
            onClose();
            onLogout();
          }}
        >
          <LogoutMenuIcon />
          {t("userMenu.logout")}
        </button>
      </div>
      {showLanguages &&
        flyoutPos &&
        createPortal(
          <div
            className={`fixed z-[100] ${panelClass}`}
            style={{ top: flyoutPos.top, left: flyoutPos.left }}
            role="menu"
            aria-label={t("userMenu.language")}
            onMouseLeave={() => setShowLanguages(false)}
          >
            {SUPPORTED_LANGUAGES.map((language) => (
              <button
                key={language}
                className={`${menuItemClass} text-[#f3f0e8]`}
                role="menuitemradio"
                aria-checked={active === language}
                type="button"
                onClick={() => chooseLanguage(language)}
              >
                <span className="min-w-0 flex-1 truncate text-left">
                  {languageLabel[language]}
                </span>
                {active === language && (
                  <Icon
                    name="check"
                    size="17px"
                    className="ml-2 shrink-0 text-[#5599e7]"
                  />
                )}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </div>
  );
}

function LogoutMenuIcon() {
  return (
    <svg
      className={menuIconClass}
      viewBox="0 0 24 24"
      aria-hidden="true"
      fill="none"
    >
      <path
        d="M14 7V5.5C14 4.7 13.3 4 12.5 4H6C5.2 4 4.5 4.7 4.5 5.5v13c0 .8.7 1.5 1.5 1.5h6.5c.8 0 1.5-.7 1.5-1.5V17"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M10 12h10m0 0-3-3m3 3-3 3"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
