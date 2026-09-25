import { expect, test } from "vitest";

import { AuthExpiredError, UserFacingError } from "../api/http";
import { describeActionError } from "./actionErrors";

test("an internal API message is replaced by the translated fallback", () => {
  expect(
    describeActionError(new Error("failed to update thread"), "Fallback"),
  ).toBe("Fallback");
  expect(describeActionError("not even an error", "Fallback")).toBe("Fallback");
});

test("a user-facing error keeps its own text", () => {
  expect(
    describeActionError(new UserFacingError("Bitte kürzen."), "Fallback"),
  ).toBe("Bitte kürzen.");
});

test("an expired session is signalled, not shown", () => {
  expect(describeActionError(new AuthExpiredError(), "Fallback")).toBeNull();
});
