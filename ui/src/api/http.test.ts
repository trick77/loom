import { afterEach, expect, test, vi } from "vitest";

import {
  AuthExpiredError,
  expectJSON,
  expectOK,
  request,
  requestJSON,
} from "./http";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("requestJSON passes plain options through and encodes a json body", async () => {
  const fetchMock = vi.fn(async () => Response.json({ ok: true }));
  vi.stubGlobal("fetch", fetchMock);

  await expect(requestJSON("/api/x", "failed")).resolves.toEqual({ ok: true });
  expect(fetchMock).toHaveBeenLastCalledWith("/api/x");

  await requestJSON("/api/x", "failed", { method: "POST" });
  expect(fetchMock).toHaveBeenLastCalledWith("/api/x", { method: "POST" });

  await requestJSON("/api/x", "failed", { method: "PATCH", json: { a: 1 } });
  expect(fetchMock).toHaveBeenLastCalledWith("/api/x", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ a: 1 }),
  });
});

test("a json body keeps the caller's own headers", async () => {
  const fetchMock = vi.fn(async () => Response.json({ ok: true }));
  vi.stubGlobal("fetch", fetchMock);

  await request("/api/x", "failed", {
    method: "POST",
    headers: { "X-Custom": "1" },
    json: { a: 1 },
  });

  expect(fetchMock).toHaveBeenLastCalledWith("/api/x", {
    method: "POST",
    headers: { "x-custom": "1", "Content-Type": "application/json" },
    body: JSON.stringify({ a: 1 }),
  });

  await request("/api/x", "failed", {
    method: "POST",
    headers: new Headers({ "X-Custom": "1" }),
    json: { a: 1 },
  });
  expect(fetchMock).toHaveBeenLastCalledWith("/api/x", {
    method: "POST",
    headers: { "x-custom": "1", "Content-Type": "application/json" },
    body: JSON.stringify({ a: 1 }),
  });
});

test("a json body replaces the caller's own content type", async () => {
  const fetchMock = vi.fn(async () => Response.json({ ok: true }));
  vi.stubGlobal("fetch", fetchMock);

  await request("/api/x", "failed", {
    method: "POST",
    headers: { "content-type": "text/plain" },
    json: { a: 1 },
  });

  expect(fetchMock).toHaveBeenLastCalledWith("/api/x", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ a: 1 }),
  });
});

test("request maps failures like expectOK and returns the response", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(new Response("blob", { status: 200 }))
    .mockResolvedValueOnce(new Response("", { status: 404 }))
    .mockResolvedValueOnce(new Response("", { status: 401 }))
    .mockResolvedValueOnce(new Response("", { status: 500 }));
  vi.stubGlobal("fetch", fetchMock);

  const response = await request("/api/x", "failed");
  await expect(response.text()).resolves.toBe("blob");
  await expect(
    request("/api/x", "failed", { method: "DELETE" }, [404]),
  ).resolves.toBeInstanceOf(Response);
  await expect(request("/api/x", "failed")).rejects.toBeInstanceOf(
    AuthExpiredError,
  );
  await expect(request("/api/x", "failed to delete x")).rejects.toThrow(
    "failed to delete x",
  );
});

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
