import { afterEach, describe, expect, test, vi } from "vitest";

import {
  AppleMusicError,
  checkAppleMusic,
  disconnectAppleMusic,
  getAppleMusicStatus,
  setAppleMusicToken,
  type AppleMusicStatus,
} from "./appleMusic";
import { AuthExpiredError } from "./http";

afterEach(() => {
  vi.unstubAllGlobals();
});

const connected: AppleMusicStatus = {
  state: "connected",
  storefront: "ch",
  playlistCount: 12,
  lastOkAt: "2026-11-01T10:00:00Z",
  setAt: "2026-10-01T10:00:00Z",
};

function stubFetch(response: Response) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("getAppleMusicStatus", () => {
  test("reads the status", async () => {
    const fetchMock = stubFetch(Response.json(connected));

    await expect(getAppleMusicStatus()).resolves.toEqual(connected);
    expect(fetchMock).toHaveBeenCalledWith("/api/me/apple-music");
  });

  test("carries the status and server text of a 503", async () => {
    stubFetch(Response.json({ error: "sidecar down" }, { status: 503 }));

    const err = await getAppleMusicStatus().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(AppleMusicError);
    expect((err as AppleMusicError).status).toBe(503);
    expect((err as AppleMusicError).message).toBe("sidecar down");
  });

  test("a 404 is an AppleMusicError too", async () => {
    stubFetch(new Response("", { status: 404 }));

    const err = await getAppleMusicStatus().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(AppleMusicError);
    expect((err as AppleMusicError).status).toBe(404);
  });

  test("a 401 means the session expired", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(getAppleMusicStatus()).rejects.toBeInstanceOf(
      AuthExpiredError,
    );
  });
});

describe("setAppleMusicToken", () => {
  test("puts the token as JSON", async () => {
    const fetchMock = stubFetch(Response.json(connected));

    await expect(setAppleMusicToken("secret")).resolves.toEqual(connected);
    expect(fetchMock).toHaveBeenCalledWith("/api/me/apple-music", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: "secret" }),
    });
  });

  test("a 422 carries the status and code, never the token", async () => {
    stubFetch(
      Response.json(
        { error: "token rejected", code: "malformed" },
        { status: 422 },
      ),
    );

    const err = await setAppleMusicToken("secret").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(AppleMusicError);
    expect((err as AppleMusicError).status).toBe(422);
    expect((err as AppleMusicError).code).toBe("malformed");
    expect((err as AppleMusicError).message).not.toContain("secret");
  });

  test("a body without JSON still yields an AppleMusicError", async () => {
    stubFetch(new Response("bad gateway", { status: 502 }));

    const err = await setAppleMusicToken("secret").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(AppleMusicError);
    expect((err as AppleMusicError).status).toBe(502);
  });
});

describe("checkAppleMusic", () => {
  test("posts to the check endpoint", async () => {
    const fetchMock = stubFetch(Response.json(connected));

    await expect(checkAppleMusic()).resolves.toEqual(connected);
    expect(fetchMock).toHaveBeenCalledWith("/api/me/apple-music/check", {
      method: "POST",
    });
  });
});

describe("disconnectAppleMusic", () => {
  test("deletes the token", async () => {
    const fetchMock = stubFetch(Response.json({ state: "never" }));

    await expect(disconnectAppleMusic()).resolves.toEqual({ state: "never" });
    expect(fetchMock).toHaveBeenCalledWith("/api/me/apple-music", {
      method: "DELETE",
    });
  });
});
