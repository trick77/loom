import { request, type RequestOptions } from "./http";

// StravaStatus mirrors GET/POST/DELETE /api/me/strava: the signed-in user's own
// connection. No Strava token ever comes back.
export type StravaStatus = {
  state: "never" | "connected" | "expired" | "error";
  athlete?: { id: number; name: string };
  scopes?: string[];
  missingScopes?: string[];
  connectedAt?: string;
  lastOkAt?: string;
  lastError?: string;
};

// StravaError is a non-2xx answer; the panel words it by code: upstream
// (502, Strava failed), storage (500), config or unavailable (503, loom's
// sidecar). 404 means the feature is not allowed. message is the server's
// text, never shown.
export class StravaError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, message: string, code = "") {
    super(message);
    this.name = "StravaError";
    this.status = status;
    this.code = code;
  }
}

const BASE = "/api/me/strava";

// STRAVA_CONNECT_URL starts Strava's OAuth: a top-level navigation, not a
// fetch, since the page goes to Strava and comes back to loom's callback.
export const STRAVA_CONNECT_URL = `${BASE}/connect`;

// The outcomes the OAuth callback sends back in ?strava=… (the panel words
// them); anything else is shown as "unavailable".
export const STRAVA_OUTCOMES = [
  "connected",
  "denied",
  "state",
  "rejected",
  "upstream",
  "storage",
  "config",
  "unavailable",
  "signin",
] as const;
export type StravaOutcome = (typeof STRAVA_OUTCOMES)[number];

const EXPECTED_FAILURES = [404, 422, 500, 502, 503];

async function call(
  url: string,
  options?: RequestOptions,
): Promise<StravaStatus> {
  const response = await request(
    url,
    "strava request failed",
    options,
    EXPECTED_FAILURES,
  );
  if (!response.ok) {
    let message = "strava request failed";
    let code = "";
    try {
      const body = (await response.json()) as {
        error?: unknown;
        code?: unknown;
      };
      if (typeof body.error === "string" && body.error !== "")
        message = body.error;
      if (typeof body.code === "string") code = body.code;
    } catch {
      // Not JSON: the generic text.
    }
    throw new StravaError(response.status, message, code);
  }
  return response.json() as Promise<StravaStatus>;
}

export function getStravaStatus(): Promise<StravaStatus> {
  return call(BASE);
}

export function checkStrava(): Promise<StravaStatus> {
  return call(`${BASE}/check`, { method: "POST" });
}

export function disconnectStrava(): Promise<StravaStatus> {
  return call(BASE, { method: "DELETE" });
}

// readStravaOutcome reads ?strava=… without touching the address bar: safe
// in a render (a useState initializer may run twice).
export function readStravaOutcome(): StravaOutcome | null {
  const raw = new URLSearchParams(window.location.search).get("strava");
  if (raw === null) return null;
  return (STRAVA_OUTCOMES as readonly string[]).includes(raw)
    ? (raw as StravaOutcome)
    : "unavailable";
}

// clearStravaOutcome removes ?strava=… from the address bar.
export function clearStravaOutcome(): void {
  const params = new URLSearchParams(window.location.search);
  if (!params.has("strava")) return;
  params.delete("strava");
  const query = params.toString();
  window.history.replaceState(
    window.history.state,
    "",
    `${window.location.pathname}${query ? `?${query}` : ""}${window.location.hash}`,
  );
}
