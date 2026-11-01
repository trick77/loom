import { request, type RequestOptions } from "./http";

// AppleMusicStatus mirrors GET/PUT/POST/DELETE /api/me/apple-music. The token
// itself never comes back: the server only reports what it learned from it.
export type AppleMusicStatus = {
  state: "never" | "connected" | "expired" | "error";
  storefront?: string;
  playlistCount?: number;
  lastOkAt?: string;
  setAt?: string;
  lastError?: string;
  // With state "error": "bridge" (Apple changed what the bridge relies on,
  // the user's token is fine) or "upstream" (Apple failed or is unreachable).
  errorKind?: "bridge" | "upstream";
};

// AppleMusicError is a non-2xx answer; the panel words it by code: malformed
// or rejected (422, the old token stays), bridge or upstream (502, Apple
// changed the player or failed), storage (500), config or unavailable (503,
// loom's sidecar). 404 means the feature is not allowed. message is the
// server's text, never shown.
export class AppleMusicError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, message: string, code = "") {
    super(message);
    this.name = "AppleMusicError";
    this.status = status;
    this.code = code;
  }
}

const BASE = "/api/me/apple-music";

// Statuses whose `{ error }` body the panel shows; anything else (and a 401,
// which request maps to AuthExpiredError) fails generically in request.
const EXPECTED_FAILURES = [404, 422, 500, 502, 503];

async function call(
  url: string,
  options?: RequestOptions,
): Promise<AppleMusicStatus> {
  const response = await request(
    url,
    "apple music request failed",
    options,
    EXPECTED_FAILURES,
  );
  if (!response.ok) {
    const { message, code } = await errorBody(response);
    throw new AppleMusicError(response.status, message, code);
  }
  return response.json() as Promise<AppleMusicStatus>;
}

async function errorBody(
  response: Response,
): Promise<{ message: string; code: string }> {
  const fallback = { message: "apple music request failed", code: "" };
  try {
    const body = (await response.json()) as { error?: unknown; code?: unknown };
    return {
      message:
        typeof body.error === "string" && body.error !== ""
          ? body.error
          : fallback.message,
      code: typeof body.code === "string" ? body.code : "",
    };
  } catch {
    // Not JSON: the generic text.
    return fallback;
  }
}

export function getAppleMusicStatus(): Promise<AppleMusicStatus> {
  return call(BASE);
}

export function setAppleMusicToken(token: string): Promise<AppleMusicStatus> {
  return call(BASE, { method: "PUT", json: { token } });
}

export function checkAppleMusic(): Promise<AppleMusicStatus> {
  return call(`${BASE}/check`, { method: "POST" });
}

export function disconnectAppleMusic(): Promise<AppleMusicStatus> {
  return call(BASE, { method: "DELETE" });
}
