import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import {
  AppleMusicError,
  checkAppleMusic,
  disconnectAppleMusic,
  getAppleMusicStatus,
  setAppleMusicToken,
  type AppleMusicStatus,
} from "../api";
import { Icon } from "../chat/Icon";
import { formatTimeAgo } from "../timeago";

type Busy = "save" | "check" | "disconnect" | null;

// statusLine renders the server's view of the stored token in one line.
function statusLine(s: AppleMusicStatus, t: TFunction): string {
  switch (s.state) {
    case "connected": {
      const parts = [t("settings.appleMusic.connected")];
      if (s.storefront) parts.push(s.storefront.toUpperCase());
      if (s.playlistCount !== undefined)
        parts.push(
          t("settings.appleMusic.playlists", { count: s.playlistCount }),
        );
      if (s.lastOkAt)
        parts.push(
          t("settings.appleMusic.lastChecked", {
            when: formatTimeAgo(s.lastOkAt),
          }),
        );
      return parts.join(" · ");
    }
    case "expired":
      return t("settings.appleMusic.expired");
    case "error":
      // Worded by kind; the sidecar's own text is internal and English.
      return s.errorKind === "bridge"
        ? t("settings.appleMusic.errorBridge")
        : t("settings.appleMusic.errorUpstream");
    default:
      return t("settings.appleMusic.never");
  }
}

// failureText maps a failed call to localized text by the server's code. The
// token is never part of it, nor is the server's own error text. stored is
// the state of the token that stays in place after a refusal.
function failureText(
  err: unknown,
  fallbackKey: string,
  t: TFunction,
  stored: AppleMusicStatus["state"] = "never",
): string {
  if (!(err instanceof AppleMusicError)) return t(fallbackKey);
  switch (err.code) {
    case "malformed":
      return t("settings.appleMusic.malformed");
    case "rejected":
      // The stored token stays: say whether it still works.
      if (stored === "connected") return t("settings.appleMusic.rejected");
      if (stored === "never") return t("settings.appleMusic.rejectedFirst");
      return t("settings.appleMusic.rejectedStale");
    case "bridge":
      return t("settings.appleMusic.errorBridge");
    case "upstream":
      return t("settings.appleMusic.unavailable");
    case "storage":
      return t("settings.appleMusic.storage");
    case "config":
      return t("settings.appleMusic.config");
  }
  if (err.status === 422) return t("settings.appleMusic.rejectedFirst");
  if (err.status === 502 || err.status === 503)
    return t("settings.appleMusic.sidecarDown");
  return t(fallbackKey);
}

// Theme tokens only (index.css @theme), so a theme change reaches the panel.
// No text colour here: each button sets its own, since two text-* classes on
// one element resolve by CSS order, not by the order they are written in.
const buttonClass =
  "inline-flex shrink-0 items-center gap-1.5 rounded-ui border border-border px-2.5 py-1 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50";

// AppleMusicPanel lets the user store their music.apple.com media-user-token.
// The token lives only in the input until it is sent; the server never returns
// it, so the panel shows status only.
export function AppleMusicPanel() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<AppleMusicStatus | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState<Busy>(null);

  useEffect(() => {
    let active = true;
    getAppleMusicStatus()
      .then((s) => active && setStatus(s))
      .catch(
        (err: unknown) =>
          active &&
          setLoadError(failureText(err, "settings.appleMusic.loadFailed", t)),
      );
    return () => {
      active = false;
    };
    // t is left out: a language switch must not refetch the status.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function run(
    kind: Exclude<Busy, null>,
    action: () => Promise<AppleMusicStatus>,
    fallbackKey: string,
  ): Promise<boolean> {
    setBusy(kind);
    setActionError(null);
    try {
      setStatus(await action());
      setLoadError(null);
      return true;
    } catch (err) {
      setActionError(failureText(err, fallbackKey, t, status?.state));
      return false;
    } finally {
      setBusy(null);
    }
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    const value = token.trim();
    if (value === "" || busy !== null) return;
    if (
      await run(
        "save",
        () => setAppleMusicToken(value),
        "settings.appleMusic.saveFailed",
      )
    )
      setToken("");
  }

  const hasToken = status !== null && status.state !== "never";

  return (
    <section>
      <h1 className="font-serif text-2xl font-light tracking-tight text-ink">
        {t("settings.appleMusic.title")}
      </h1>
      <p className="mt-1 text-sm text-muted">
        {t("settings.appleMusic.description")}
      </p>

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
          {hasToken && (
            <button
              type="button"
              className={`${buttonClass} text-ink hover:bg-active`}
              disabled={busy !== null}
              onClick={() =>
                void run(
                  "check",
                  checkAppleMusic,
                  "settings.appleMusic.checkFailed",
                )
              }
            >
              <Icon
                name={busy === "check" ? "spinner" : "retry"}
                size="14px"
                className={busy === "check" ? "animate-spin" : ""}
              />
              {busy === "check"
                ? t("settings.appleMusic.checking")
                : t("settings.appleMusic.checkNow")}
            </button>
          )}
          {hasToken && (
            <button
              type="button"
              className={`${buttonClass} text-danger hover:bg-danger hover:text-ink`}
              disabled={busy !== null}
              onClick={() =>
                void run(
                  "disconnect",
                  disconnectAppleMusic,
                  "settings.appleMusic.disconnectFailed",
                )
              }
            >
              {busy === "disconnect" && (
                <Icon name="spinner" size="14px" className="animate-spin" />
              )}
              {busy === "disconnect"
                ? t("settings.appleMusic.disconnecting")
                : t("settings.appleMusic.disconnect")}
            </button>
          )}
        </div>
      )}

      <div className="mt-6 text-sm font-medium text-muted">
        {t("settings.appleMusic.helpTitle")}
      </div>
      <ol className="mt-2 list-decimal space-y-1 pl-5 text-sm text-ink">
        <li>{t("settings.appleMusic.step1")}</li>
        <li>{t("settings.appleMusic.step2")}</li>
        <li>{t("settings.appleMusic.step3")}</li>
        <li>{t("settings.appleMusic.step4")}</li>
      </ol>

      <form className="mt-4 flex flex-col gap-2" onSubmit={(e) => void save(e)}>
        <label
          htmlFor="apple-music-token"
          className="font-mono text-xs text-muted"
        >
          {t("settings.appleMusic.tokenLabel")}
        </label>
        <div className="flex gap-2">
          <input
            id="apple-music-token"
            type="password"
            autoComplete="off"
            spellCheck={false}
            className="min-w-0 flex-1 rounded-ui border border-border bg-bg px-3 py-1.5 font-mono text-sm text-ink placeholder:text-muted focus:border-accent focus:outline-none"
            placeholder={t("settings.appleMusic.tokenPlaceholder")}
            value={token}
            disabled={busy === "save"}
            onChange={(event) => setToken(event.target.value)}
          />
          <button
            type="submit"
            className="inline-flex shrink-0 items-center gap-1.5 rounded-ui bg-accent px-3 py-1.5 text-sm text-ink transition-colors hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-45"
            disabled={token.trim() === "" || busy !== null}
          >
            {busy === "save" && (
              <Icon name="spinner" size="14px" className="animate-spin" />
            )}
            {busy === "save"
              ? t("settings.appleMusic.saving")
              : t("settings.appleMusic.save")}
          </button>
        </div>
        {actionError !== null && (
          <p className="text-sm text-danger" role="alert">
            {actionError}
          </p>
        )}
      </form>
      <p className="mt-3 text-xs text-muted">
        {t("settings.appleMusic.privacy")}
      </p>
    </section>
  );
}
