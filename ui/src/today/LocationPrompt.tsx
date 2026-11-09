import { useId, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import { PlaceNotFoundError, setLocation } from "../api";
import { UserFacingError } from "../api/http";

// LocationPrompt asks where the user lives when no memory says so. The place
// is saved as a memory; onSaved reloads the section with its weather.
export function LocationPrompt({ onSaved }: { onSaved(): void }) {
  const { t } = useTranslation();
  const inputId = useId();
  const errorId = useId();
  const [place, setPlace] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent) {
    event.preventDefault();
    const value = place.trim();
    if (value === "" || saving) return;
    setSaving(true);
    setError("");
    try {
      await setLocation(value);
      onSaved();
    } catch (err) {
      setError(
        err instanceof PlaceNotFoundError
          ? t("today.location.notFound")
          : err instanceof UserFacingError && err.message !== ""
            ? err.message
            : t("today.location.saveFailed"),
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <form
      className="mx-auto flex w-full max-w-[480px] flex-col items-center gap-2.5"
      onSubmit={(event) => void save(event)}
    >
      <label className="text-center text-[13px] text-muted" htmlFor={inputId}>
        {t("today.location.ask")}
      </label>
      <div className="flex w-full gap-2">
        <input
          aria-describedby={error === "" ? undefined : errorId}
          aria-invalid={error === "" ? undefined : true}
          autoComplete="address-level2"
          className="h-10 min-w-0 flex-1 rounded-[10px] border border-border bg-card px-3.5 text-sm text-ink outline-none placeholder:text-muted focus:border-border-strong"
          id={inputId}
          onChange={(event) => setPlace(event.target.value)}
          placeholder={t("today.location.placeholder")}
          type="text"
          value={place}
        />
        <button
          className="h-10 shrink-0 rounded-[10px] bg-[rgba(255,255,255,0.1)] px-3.5 text-sm text-ink transition-colors hover:bg-[rgba(255,255,255,0.16)] disabled:opacity-50"
          disabled={saving || place.trim() === ""}
          type="submit"
        >
          {t("today.location.save")}
        </button>
      </div>
      {error !== "" && (
        <div className="text-[13px] text-danger" id={errorId} role="alert">
          {error}
        </div>
      )}
      <span className="text-center text-xs text-muted">
        {t("today.location.note")}
      </span>
    </form>
  );
}
