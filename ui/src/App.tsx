import { useCallback, useEffect, useState } from "react";
import { ThreadShell } from "./ThreadShell";
import loomLogo from "./assets/loom-logo.svg";
import { getMe, listUsers, logout, type User } from "./api";

const TAGLINES = [
  "Weave your thoughts, thread by thread.",
  "Pull a thread. See where it leads.",
  "Every thought has another thread.",
  "Where every idea forks.",
  "One prompt, a thousand threads.",
  "Follow the thread. Find the thought.",
  "Think in threads.",
  "Wander every branch of thought.",
  "Untangle. Explore. Weave.",
  "Where conversations branch.",
  "Spin ideas into threads.",
  "A loom for a branching mind.",
];

type Status = "loading" | "signed-out" | "ready" | "error";

export default function App() {
  const [status, setStatus] = useState<Status>("loading");
  const [user, setUser] = useState<User | null>(null);
  const [adminUsers, setAdminUsers] = useState<User[]>([]);
  const [showAdmin, setShowAdmin] = useState(false);
  const [tagline] = useState(() => TAGLINES[Math.floor(Math.random() * TAGLINES.length)]);

  useEffect(() => {
    let active = true;
    getMe()
      .then((currentUser) => {
        if (!active) return;
        setUser(currentUser);
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
        Loading
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
            Sign in
          </a>
        </section>
      </main>
    );
  }

  if (status === "error" || user === null) {
    return (
      <div className="flex h-svh items-center justify-center bg-bg font-sans text-ink">
        Service unavailable
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
          <h1 className="font-serif text-2xl font-light tracking-tight">Admin</h1>
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
