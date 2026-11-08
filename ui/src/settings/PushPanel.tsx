import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { PushTestError, getPushConfig, sendTestPush } from "../api";
import { Icon } from "../chat/Icon";
import {
  PushPermissionError,
  currentPushSubscription,
  disablePush,
  enablePush,
  pushSupport,
} from "../pushDevice";

type Busy = "enable" | "disable" | "test" | null;

const buttonClass =
  "inline-flex shrink-0 items-center gap-1.5 rounded-ui border border-border px-2.5 py-1 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50";

function failureKey(err: unknown, fallbackKey: string): string {
  // A dismissed prompt ("default") is not a block: another tap asks again.
  if (err instanceof PushPermissionError)
    return err.permission === "denied"
      ? "settings.push.blocked"
      : "settings.push.notAllowed";
  if (err instanceof PushTestError)
    return err.status === 502
      ? "settings.push.testRefused"
      : "settings.push.testGone";
  return fallbackKey;
}

// PushPanel turns notifications on or off for this device only: each browser
// holds its own subscription.
export function PushPanel() {
  const { t } = useTranslation();
  const support = pushSupport();
  const [publicKey, setPublicKey] = useState<string | null>(null);
  const [endpoint, setEndpoint] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [busy, setBusy] = useState<Busy>(null);
  const [errorKey, setErrorKey] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [blocked, setBlocked] = useState(
    () => support === "supported" && Notification.permission === "denied",
  );

  useEffect(() => {
    if (support !== "supported") return;
    let active = true;
    Promise.all([getPushConfig(), currentPushSubscription()])
      .then(([config, sub]) => {
        if (!active) return;
        setPublicKey(config.publicKey);
        setEndpoint(sub?.endpoint ?? null);
        setLoaded(true);
      })
      .catch(() => active && setLoadError(true));
    return () => {
      active = false;
    };
  }, [support]);

  async function run(
    kind: Exclude<Busy, null>,
    action: () => Promise<void>,
    fallbackKey: string,
  ) {
    setBusy(kind);
    setErrorKey(null);
    setNotice(null);
    try {
      await action();
    } catch (err) {
      if (err instanceof PushPermissionError && err.permission === "denied")
        setBlocked(true);
      setErrorKey(failureKey(err, fallbackKey));
    } finally {
      setBusy(null);
    }
  }

  // The permission prompt must open inside the tap: enablePush is called
  // before anything else awaits.
  function enable() {
    if (publicKey === null) return;
    void run(
      "enable",
      async () => {
        const sub = await enablePush(publicKey);
        setEndpoint(sub.endpoint);
      },
      "settings.push.enableFailed",
    );
  }

  function disable() {
    void run(
      "disable",
      async () => {
        try {
          await disablePush();
        } finally {
          // The browser may be off even when the server did not answer.
          const sub = await currentPushSubscription().catch(() => null);
          setEndpoint(sub?.endpoint ?? null);
        }
      },
      "settings.push.disableFailed",
    );
  }

  function test() {
    if (endpoint === null) return;
    void run(
      "test",
      async () => {
        await sendTestPush(
          endpoint,
          t("settings.push.testTitle"),
          t("settings.push.testBody"),
        );
        setNotice(t("settings.push.testSent"));
      },
      "settings.push.testFailed",
    );
  }

  return (
    <section>
      <h1 className="font-serif text-2xl font-light tracking-tight text-ink">
        {t("settings.push.title")}
      </h1>
      <p className="mt-1 text-sm text-muted">
        {t("settings.push.description")}
      </p>

      {support === "needsInstall" && (
        <p className="mt-4 text-sm text-ink">
          {t("settings.push.needsInstall")}
        </p>
      )}
      {support === "unsupported" && (
        <p className="mt-4 text-sm text-muted">
          {t("settings.push.unsupported")}
        </p>
      )}
      {support === "supported" && blocked && endpoint === null && (
        <p className="mt-4 text-sm text-danger">{t("settings.push.blocked")}</p>
      )}
      {support === "supported" && loadError && (
        <p className="mt-4 text-sm text-danger">
          {t("settings.push.loadFailed")}
        </p>
      )}
      {support === "supported" && !loaded && !loadError && (
        <p className="mt-4 text-sm text-muted">{t("settings.loading")}</p>
      )}

      {support === "supported" && loaded && (
        <div className="mt-4 flex flex-wrap items-center gap-3 border-y border-border py-3">
          <p
            className={`min-w-0 flex-1 text-sm ${endpoint ? "text-ink" : "text-muted"}`}
          >
            {endpoint ? t("settings.push.on") : t("settings.push.off")}
          </p>
          {endpoint === null && !blocked && (
            <button
              type="button"
              className="inline-flex items-center gap-1.5 rounded-ui bg-accent px-3 py-1.5 text-sm text-ink transition-colors hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-50"
              disabled={busy !== null}
              onClick={enable}
            >
              {busy === "enable" && (
                <Icon name="spinner" size="14px" className="animate-spin" />
              )}
              {t("settings.push.enable")}
            </button>
          )}
          {endpoint !== null && (
            <button
              type="button"
              className={`${buttonClass} text-ink hover:bg-active`}
              disabled={busy !== null}
              onClick={test}
            >
              {busy === "test" && (
                <Icon name="spinner" size="14px" className="animate-spin" />
              )}
              {t("settings.push.test")}
            </button>
          )}
          {endpoint !== null && (
            <button
              type="button"
              className={`${buttonClass} text-danger hover:bg-danger hover:text-ink`}
              disabled={busy !== null}
              onClick={disable}
            >
              {busy === "disable" && (
                <Icon name="spinner" size="14px" className="animate-spin" />
              )}
              {t("settings.push.disable")}
            </button>
          )}
        </div>
      )}

      {notice !== null && (
        <p className="mt-3 text-sm text-ink" role="status">
          {notice}
        </p>
      )}
      {errorKey !== null && (
        <p className="mt-3 text-sm text-danger" role="alert">
          {t(errorKey)}
        </p>
      )}
      <p className="mt-3 text-xs text-muted">{t("settings.push.privacy")}</p>
    </section>
  );
}
