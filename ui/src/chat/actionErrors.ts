import { AuthExpiredError, UserFacingError } from "../api/http";

// describeActionError picks the text a failed action shows. Only an error
// written for the user (a localized validation message, the server's own
// stream error) is shown verbatim; every other error, including the API
// layer's internal English messages, falls back to the caller's translated
// text. null means the session expired and the caller should sign out
// instead of showing anything.
export function describeActionError(
  error: unknown,
  fallback: string,
): string | null {
  if (error instanceof AuthExpiredError) return null;
  if (error instanceof UserFacingError && error.message !== "") {
    return error.message;
  }
  return fallback;
}
