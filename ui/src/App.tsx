import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ThreadShell } from "./ThreadShell";
import loomLogo from "./assets/loom-logo.svg";
import { getMe, listUsers, logout, updateMe, type User } from "./api";
import { applyUserLanguage, seedLanguageFor } from "./i18n";

type Status = "loading" | "signed-out" | "ready" | "error";

export default function App() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<Status>("loading");
  const [user, setUser] = useState<User | null>(null);
  const [adminUsers, setAdminUsers] = useState<User[]>([]);
  const [showAdmin, setShowAdmin] = useState(false);
  const taglines = t("app.taglines", { returnObjects: true }) as string[];
  const [taglineIndex] = useState(() => Math.floor(Math.random() * taglines.length));
  const tagline = taglines[taglineIndex] ?? taglines[0];

  useEffect(() => {
    let active = true;
    getMe()
      .then((currentUser) => {
        if (!active) return;
        // A signed-in profile with no pinned language (unset, or a legacy `auto`)
        // is seeded from the browser locale once, so the language stops being
        // unset and drives the LLM answer language. Applied optimistically; the
        // PATCH is best-effort and the optimistic value stays on failure.
        const seed = currentUser ? seedLanguageFor(currentUser.responseLanguage) : null;
        if (currentUser && seed) {
          applyUserLanguage(seed);
          setUser({ ...currentUser, responseLanguage: seed });
          void updateMe({ responseLanguage: seed }).catch(() => {});
        } else {
          applyUserLanguage(currentUser?.responseLanguage);
          setUser(currentUser);
        }
        setStatus(currentUser ? "ready" : "signed-out");
      })
      .catch(() => {
        if (!active) return;
        setStatus("error");
      });
    return () => {
      active = false;
    };
  }, []);

  async function handleLogout() {
    try {
      const redirectUrl = await logout();
      window.location.assign(redirectUrl);
    } catch {
      setStatus("signed-out");
      setUser(null);
    }
  }

  async function handleAdmin() {
    setShowAdmin(true);
    if (adminUsers.length === 0) {
      try {
        setAdminUsers(await listUsers());
      } catch {
        setStatus("signed-out");
        setUser(null);
      }
    }
  }

  function handleThread() {
    setShowAdmin(false);
  }

  const handleSessionExpired = useCallback(() => {
    setStatus("signed-out");
    setUser(null);
  }, []);

  if (status === "loading") {
    return (
      <div className="flex h-svh items-center justify-center bg-bg font-sans text-muted">
        {t("app.loading")}
      </div>
    );
  }

  if (status === "signed-out") {
    return (
      <main className="flex h-svh items-center justify-center bg-bg px-6 font-sans text-ink">
        <section className="flex w-full max-w-md flex-col items-center gap-5 text-center">
          <div className="flex items-center gap-3">
            <img src={loomLogo} alt="" aria-hidden className="h-16 w-16" />
            <span className="font-serif font-medium leading-none text-[64px] text-[#f4f3ee]">Loom</span>
          </div>
          <p className="-mt-2 whitespace-nowrap font-sans text-xl text-muted">{tagline}</p>
          <a
            href="/api/auth/login"
            className="mt-6 rounded-ui bg-accent px-5 py-2.5 text-sm font-medium text-white transition-colors hover:bg-accent-strong"
          >
            {t("app.signIn")}
          </a>
        </section>
      </main>
    );
  }

  if (status === "error" || user === null) {
    return (
      <div className="flex h-svh items-center justify-center bg-bg font-sans text-ink">
        {t("app.serviceUnavailable")}
      </div>
    );
  }

  return (
    <ThreadShell
      user={user}
      showAdmin={showAdmin}
      onAdmin={handleAdmin}
      onThread={handleThread}
      onLogout={handleLogout}
      onSessionExpired={handleSessionExpired}
      adminPanel={
        <section className="h-full overflow-y-auto p-6">
          <h1 className="font-serif text-2xl font-light tracking-tight">{t("app.admin")}</h1>
          <div className="mt-4 divide-y divide-border border-y border-border">
            {adminUsers.map((adminUser) => (
              <div key={adminUser.id} className="flex justify-between py-3 text-sm">
                <span>{adminUser.displayName || adminUser.username}</span>
                <span className="text-muted capitalize">{adminUser.role}</span>
              </div>
            ))}
          </div>
        </section>
      }
    />
  );
}
