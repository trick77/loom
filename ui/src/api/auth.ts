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
