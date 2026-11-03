import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import {
  STRAVA_CONNECT_URL,
  StravaError,
  checkStrava,
  disconnectStrava,
  getStravaStatus,
  type StravaOutcome,
  type StravaStatus,
} from "../api";
import { Icon } from "../chat/Icon";
import { formatTimeAgo } from "../timeago";

type Busy = "check" | "disconnect" | null;

// statusLine renders the server's view of the user's connection in one line.
function statusLine(s: StravaStatus, t: TFunction): string {
  switch (s.state) {
    case "connected": {
      const parts = [
        s.athlete?.name
          ? t("settings.strava.connected", { name: s.athlete.name })
          : t("settings.strava.connectedNoName"),
      ];
      if (s.lastOkAt)
        parts.push(
          t("settings.strava.lastChecked", { when: formatTimeAgo(s.lastOkAt) }),
        );
      return parts.join(" · ");
    }
    case "expired":
      return t("settings.strava.expired");
    case "error":
      return t("settings.strava.error");
    default:
      return t("settings.strava.never");
  }
}

// failureText maps a failed call to localized text by the server's code; the
// server's own text is never shown.
function failureText(err: unknown, fallbackKey: string, t: TFunction): string {
  if (err instanceof StravaError) {
    switch (err.code) {
      case "upstream":
      case "storage":
      case "config":
      case "unavailable":
        return t(`settings.strava.outcome_${err.code}`);
    }
    if (err.status === 502 || err.status === 503)
      return t("settings.strava.outcome_unavailable");
  }
  return t(fallbackKey);
}

const buttonClass =
  "inline-flex shrink-0 items-center gap-1.5 rounded-ui border border-border px-2.5 py-1 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50";

// StravaPanel connects the signed-in user's own Strava account through
// Strava's consent page. Tokens never reach the browser; the panel shows
// status only. outcome is what the OAuth callback reported (?strava=…).
export function StravaPanel({
  outcome = null,
}: {
  outcome?: StravaOutcome | null;
}) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<StravaStatus | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busy, setBusy] = useState<Busy>(null);

  useEffect(() => {
    let active = true;
    getStravaStatus()
      .then((s) => active && setStatus(s))
      .catch(
        (err: unknown) =>
          active &&
          setLoadError(failureText(err, "settings.strava.loadFailed", t)),
      );
    return () => {
      active = false;
    };
    // t is left out: a language switch must not refetch the status.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function run(
    kind: Exclude<Busy, null>,
    action: () => Promise<StravaStatus>,
    fallbackKey: string,
  ) {
    setBusy(kind);
    setActionError(null);
    try {
      setStatus(await action());
      setLoadError(null);
    } catch (err) {
      setActionError(failureText(err, fallbackKey, t));
    } finally {
      setBusy(null);
    }
  }

  const connected = status !== null && status.state !== "never";
  const missing = status?.missingScopes ?? [];

  return (
    <section>
      <h1 className="font-serif text-2xl font-light tracking-tight text-ink">
        {t("settings.strava.title")}
      </h1>
      <p className="mt-1 text-sm text-muted">
        {t("settings.strava.description")}
      </p>

      {outcome !== null && (
        <p
          className={`mt-4 text-sm ${outcome === "connected" ? "text-ink" : "text-danger"}`}
          role="status"
        >
          {t(`settings.strava.outcome_${outcome}`)}
        </p>
      )}
      {loadError !== null && (
        <p className="mt-4 text-sm text-danger">{loadError}</p>
      )}
      {loadError === null && status === null && (
        <p className="mt-4 text-sm text-muted">{t("settings.loading")}</p>
      )}

      {status !== null && (
        <div className="mt-4 flex flex-wrap items-center gap-3 border-y border-border py-3">
          <p
            className={`min-w-0 flex-1 text-sm ${
              status.state === "connected"
                ? "text-ink"
                : status.state === "never"
                  ? "text-muted"
                  : "text-danger"
            }`}
          >
            {statusLine(status, t)}
          </p>
          {connected && (
            <button
              type="button"
              className={`${buttonClass} text-ink hover:bg-active`}
              disabled={busy !== null}
              onClick={() =>
                void run("check", checkStrava, "settings.strava.checkFailed")
              }
            >
              <Icon
                name={busy === "check" ? "spinner" : "retry"}
                size="14px"
                className={busy === "check" ? "animate-spin" : ""}
              />
              {busy === "check"
                ? t("settings.strava.checking")
                : t("settings.strava.checkNow")}
            </button>
          )}
          {connected && (
            <button
              type="button"
              className={`${buttonClass} text-danger hover:bg-danger hover:text-ink`}
              disabled={busy !== null}
              onClick={() =>
                void run(
                  "disconnect",
                  disconnectStrava,
                  "settings.strava.disconnectFailed",
                )
              }
            >
              {busy === "disconnect" && (
                <Icon name="spinner" size="14px" className="animate-spin" />
              )}
              {busy === "disconnect"
                ? t("settings.strava.disconnecting")
                : t("settings.strava.disconnect")}
            </button>
          )}
        </div>
      )}

      {missing.length > 0 && status?.state === "connected" && (
        <p className="mt-3 text-sm text-danger">
          {t("settings.strava.missingScopes", { scopes: missing.join(", ") })}
        </p>
      )}

      {status !== null &&
        (status.state !== "connected" || missing.length > 0) && (
          <a
            href={STRAVA_CONNECT_URL}
            className="mt-4 inline-flex items-center gap-1.5 rounded-ui bg-accent px-3 py-1.5 text-sm text-ink transition-colors hover:bg-accent-strong"
          >
            {status.state === "never"
              ? t("settings.strava.connect")
              : t("settings.strava.reconnect")}
          </a>
        )}
      {actionError !== null && (
        <p className="mt-3 text-sm text-danger" role="alert">
          {actionError}
        </p>
      )}
      <p className="mt-3 text-xs text-muted">{t("settings.strava.privacy")}</p>
    </section>
  );
}
