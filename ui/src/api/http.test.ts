import { expect, test } from "vitest";

import { AuthExpiredError, expectJSON, expectOK } from "./http";

test("expectJSON maps a 401 to AuthExpiredError and other failures to the caller's message", async () => {
  await expect(
    expectJSON(new Response("", { status: 401 }), "failed"),
  ).rejects.toBeInstanceOf(AuthExpiredError);
  await expect(
    expectJSON(new Response("", { status: 500 }), "failed to load x"),
  ).rejects.toThrow("failed to load x");
  await expect(
    expectJSON(Response.json({ ok: true }), "failed"),
  ).resolves.toEqual({ ok: true });
});

test("expectOK accepts empty successes, tolerated statuses, and maps the rest", async () => {
  await expect(
    expectOK(new Response(null, { status: 204 }), "failed"),
  ).resolves.toBeUndefined();
  await expect(
    expectOK(new Response("", { status: 404 }), "failed", [404]),
  ).resolves.toBeUndefined();
  await expect(
    expectOK(new Response("", { status: 401 }), "failed"),
  ).rejects.toBeInstanceOf(AuthExpiredError);
  await expect(
    expectOK(new Response("", { status: 500 }), "failed to delete x"),
  ).rejects.toThrow("failed to delete x");
});
