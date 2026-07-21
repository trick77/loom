import type { User } from "./types";

export async function getMe(): Promise<User | null> {
  const response = await fetch("/api/me");
  if (response.status === 401) {
    return null;
  }
  if (!response.ok) {
    throw new Error("failed to load current user");
  }
  return response.json();
}

// updateMe persists profile preferences. responseLanguage is the coupled UI +
// LLM answer language: only 'en'/'de' are accepted and both pin the UI locale and
// the default answer language (which still yields to an explicit in-message
// request). An unset profile is seeded from the browser locale on first visit.
export async function updateMe(patch: {
  responseLanguage: string;
}): Promise<User> {
  const response = await fetch("/api/me", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!response.ok) {
    throw new Error("failed to update profile");
  }
  return response.json();
}

export async function listUsers(): Promise<User[]> {
  const response = await fetch("/api/admin/users");
  if (!response.ok) {
    throw new Error("failed to load users");
  }
  return response.json();
}

export async function logout(): Promise<string> {
  const response = await fetch("/api/auth/logout", { method: "POST" });
  if (!response.ok) {
    throw new Error("failed to log out");
  }
  const body = (await response.json()) as { redirectUrl?: string };
  return body.redirectUrl ?? "/";
}
