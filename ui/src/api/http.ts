// AuthExpiredError signals a 401 from an authenticated endpoint — the app
// treats it as "session expired" and redirects to sign-in.
export class AuthExpiredError extends Error {
  constructor() {
    super("auth expired");
  }
}

// expectJSON is the shared success/JSON path for authenticated endpoints: it
// maps a 401 to AuthExpiredError, any other non-2xx to the caller's message,
// and otherwise decodes the JSON body.
export async function expectJSON<T>(response: Response, errorMessage: string): Promise<T> {
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error(errorMessage);
  }
  return response.json() as Promise<T>;
}
