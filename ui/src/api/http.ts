// AuthExpiredError signals a 401 from an authenticated endpoint — the app
// treats it as "session expired" and redirects to sign-in.
export class AuthExpiredError extends Error {
  constructor() {
    super("auth expired");
  }
}

// UserFacingError carries text written for the user: a localized validation
// message, or the server's own error text. Everything else the API layer
// throws is an internal English message that the UI replaces with a
// translated fallback (see chat/actionErrors.ts).
export class UserFacingError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "UserFacingError";
  }
}

// expectJSON is the shared success/JSON path for authenticated endpoints: it
// maps a 401 to AuthExpiredError, any other non-2xx to the caller's message,
// and otherwise decodes the JSON body.
export async function expectJSON<T>(
  response: Response,
  errorMessage: string,
): Promise<T> {
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error(errorMessage);
  }
  return response.json() as Promise<T>;
}

// expectOK is expectJSON for endpoints that answer with no body (204 and the
// like): it maps a 401 to AuthExpiredError and any other non-2xx to the
// caller's message. `tolerate` lists statuses that count as success (a 404 on
// an idempotent delete).
export async function expectOK(
  response: Response,
  errorMessage: string,
  tolerate: number[] = [],
): Promise<void> {
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok && !tolerate.includes(response.status)) {
    throw new Error(errorMessage);
  }
}

// RequestOptions is RequestInit plus `json`: a body that is stringified and
// sent with `Content-Type: application/json`.
export type RequestOptions = Omit<RequestInit, "body"> & { json?: unknown };

// request sends to an authenticated endpoint and checks the answer (see
// expectOK); it returns the response for a caller that reads the body.
export async function request(
  url: string,
  errorMessage: string,
  options?: RequestOptions,
  tolerate: number[] = [],
): Promise<Response> {
  const { json, ...init } = options ?? {};
  // Keeps fetch's single-argument form, which the tests and callers rely on.
  const response = await (options === undefined
    ? fetch(url)
    : fetch(url, json === undefined ? init : withJSONBody(init, json)));
  await expectOK(response, errorMessage, tolerate);
  return response;
}

// withJSONBody normalizes the caller's headers (a record, a Headers instance or
// tuples; keys come out lowercase) so its own content type cannot survive next
// to ours.
function withJSONBody(init: RequestInit, json: unknown): RequestInit {
  const headers = Object.fromEntries(new Headers(init.headers));
  delete headers["content-type"];
  headers["Content-Type"] = "application/json";
  return { ...init, headers, body: JSON.stringify(json) };
}

// requestJSON is request plus decoding the JSON answer.
export async function requestJSON<T>(
  url: string,
  errorMessage: string,
  options?: RequestOptions,
): Promise<T> {
  const response = await request(url, errorMessage, options);
  return response.json() as Promise<T>;
}
