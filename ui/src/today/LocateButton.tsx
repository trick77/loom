import { useTranslation } from "react-i18next";

// LocateButton is "use my current location", with the crosshair.
export function LocateButton({
  locating,
  disabled = false,
  onClick,
}: {
  locating: boolean;
  disabled?: boolean;
  onClick(): void;
}) {
  const { t } = useTranslation();
  return (
    <button
      className="inline-flex items-center gap-1.5 text-[13px] text-muted transition-colors hover:text-ink disabled:opacity-50"
      disabled={locating || disabled}
      onClick={onClick}
      type="button"
    >
      <svg
        aria-hidden="true"
        className="h-3.5 w-3.5"
        fill="none"
        stroke="currentColor"
        strokeLinecap="round"
        strokeWidth="1.6"
        viewBox="0 0 16 16"
      >
        <circle cx="8" cy="8" r="4.5" />
        <circle cx="8" cy="8" fill="currentColor" r="1.5" stroke="none" />
        <path d="M8 1v2M8 13v2M1 8h2M13 8h2" />
      </svg>
      {locating ? t("today.location.locating") : t("today.location.useCurrent")}
    </button>
  );
}
