import { useId, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import {
  coarsePoint,
  locateTown,
  PlaceNotFoundError,
  setLocation,
  type GeoPoint,
  type LocatedTown,
} from "../api";
import { UserFacingError } from "../api/http";

// A town is all that is needed: no GPS fix, a position up to ten minutes old
// will do.
const positionOptions: PositionOptions = {
  enableHighAccuracy: false,
  maximumAge: 10 * 60 * 1000,
  timeout: 15 * 1000,
};

const geolocationDenied = 1;

// positionWait bounds the whole ask: the browser's timeout starts only once
// the user allowed it, and a prompt dismissed without an answer (some
// browsers on "not now") never calls back.
const positionWait = 30 * 1000;

function currentPosition(): Promise<GeolocationPosition> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(
      () => reject({ code: 3 } as GeolocationPositionError),
      positionWait,
    );
    navigator.geolocation.getCurrentPosition(
      (position) => {
        window.clearTimeout(timer);
        resolve(position);
      },
      (err) => {
        window.clearTimeout(timer);
        reject(err);
      },
      positionOptions,
    );
  });
}

// A located town as the field shows it, with the point it was named for.
type Located = LocatedTown & { point: GeoPoint };

// LocationPrompt asks where the user lives when no memory says so. The place
// is saved as a memory; onSaved reloads the section with its weather. With
// canLocate the browser's position can fill in the town: the user sees it
// before saving, a position taken on a trip is no home.
export function LocationPrompt({
  onSaved,
  canLocate = false,
}: {
  onSaved(): void;
  canLocate?: boolean;
}) {
  const { t } = useTranslation();
  const inputId = useId();
  const errorId = useId();
  const [place, setPlace] = useState("");
  const [saving, setSaving] = useState(false);
  const [locating, setLocating] = useState(false);
  const [error, setError] = useState("");
  // located is the town the position named while the field still holds its
  // name: saved with its point, so the name finds that town and not
  // another of the same name.
  const [located, setLocated] = useState<Located | null>(null);
  const offerLocate =
    canLocate && typeof navigator !== "undefined" && "geolocation" in navigator;

  async function save(event: FormEvent) {
    event.preventDefault();
    const value = place.trim();
    if (value === "" || saving) return;
    setSaving(true);
    setError("");
    try {
      if (located && value === located.place.name) {
        await setLocation(value, located.point);
      } else {
        await setLocation(value);
      }
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

  async function locate() {
    if (locating) return;
    setLocating(true);
    setError("");
    let position: GeolocationPosition;
    try {
      position = await currentPosition();
    } catch (err) {
      setError(
        (err as GeolocationPositionError | undefined)?.code ===
          geolocationDenied
          ? t("today.location.locateDenied")
          : t("today.location.locateFailed"),
      );
      setLocating(false);
      return;
    }
    try {
      const point = coarsePoint(
        position.coords.latitude,
        position.coords.longitude,
      );
      const town = await locateTown(point);
      setPlace(town.place.name);
      setLocated({ ...town, point });
    } catch (err) {
      // Anything but no town is the map provider busy or away: a retry may
      // answer.
      setError(
        err instanceof PlaceNotFoundError
          ? t("today.location.locateNoTown")
          : t("today.location.locateRetry"),
      );
    } finally {
      setLocating(false);
    }
  }

  const shownTown =
    located && place.trim() === located.place.name ? located : null;

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
      {offerLocate && (
        <button
          className="inline-flex items-center gap-1.5 text-[13px] text-muted transition-colors hover:text-ink disabled:opacity-50"
          disabled={locating || saving}
          onClick={() => void locate()}
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
          {locating
            ? t("today.location.locating")
            : t("today.location.useCurrent")}
        </button>
      )}
      {shownTown && (
        <div className="flex flex-col items-center gap-0.5 text-center text-xs text-muted">
          <span>
            {[
              shownTown.place.name,
              shownTown.place.region,
              shownTown.place.country,
            ]
              .filter((part) => part)
              .join(", ")}
          </span>
          {shownTown.attribution.url ? (
            <a
              className="underline-offset-2 hover:underline"
              href={shownTown.attribution.url}
              rel="noreferrer"
              target="_blank"
            >
              {shownTown.attribution.text}
            </a>
          ) : (
            <span>{shownTown.attribution.text}</span>
          )}
        </div>
      )}
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
