import { useCallback, useEffect, useState } from "react";
import { ChatShell } from "./ChatShell";
import loomImage from "./assets/mynd-logo.png";
import { getMe, listUsers, logout, type User } from "./api";

type Status = "loading" | "signed-out" | "ready" | "error";

export default function App() {
  const [status, setStatus] = useState<Status>("loading");
  const [user, setUser] = useState<User | null>(null);
  const [adminUsers, setAdminUsers] = useState<User[]>([]);
  const [showAdmin, setShowAdmin] = useState(false);

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

  function handleChat() {
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
          <img src={loomImage} alt="Loom" className="w-full max-w-sm rounded-ui" />
          <p className="-mt-2 whitespace-nowrap font-sans text-xl text-muted">What&apos;s on your Mynd today?</p>
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
    <ChatShell
      user={user}
      showAdmin={showAdmin}
      onAdmin={handleAdmin}
      onChat={handleChat}
      onLogout={handleLogout}
      onSessionExpired={handleSessionExpired}
      adminPanel={
        <section className="p-6">
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
