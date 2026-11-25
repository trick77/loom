import { afterEach, expect, test, vi } from "vitest";

import { getArtifactSummary, listArtifacts } from "./artifacts";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(response: Response) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

test("listArtifacts sends the period bounds and leaves unset ones out", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    void input;
    return Response.json({ items: [], nextCursor: null });
  });
  vi.stubGlobal("fetch", fetchMock);
  await listArtifacts({
    type: "images",
    since: "2026-10-01T00:00:00.000Z",
    until: "2026-11-01T00:00:00.000Z",
    limit: 200,
    cursor: "c1",
  });
  const url = new URL(String(fetchMock.mock.calls[0][0]), "http://x");
  expect(url.pathname).toBe("/api/artifacts");
  expect(Object.fromEntries(url.searchParams)).toEqual({
    type: "images",
    since: "2026-10-01T00:00:00.000Z",
    until: "2026-11-01T00:00:00.000Z",
    limit: "200",
    cursor: "c1",
  });

  await listArtifacts({ search: "robot", since: "", until: undefined });
  expect(String(fetchMock.mock.calls[1][0])).toBe(
    "/api/artifacts?search=robot",
  );
});

function stubTimeZone(timeZone: string | undefined) {
  const real = Intl.DateTimeFormat.prototype.resolvedOptions;
  vi.spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions").mockImplementation(
    function (this: Intl.DateTimeFormat) {
      return {
        ...real.call(this),
        timeZone,
      } as Intl.ResolvedDateTimeFormatOptions;
    },
  );
}

test("getArtifactSummary asks with the window start, the type and the zone", async () => {
  // The page lists a month by its local bounds, so the server cuts the
  // summary's months in the browser's zone.
  stubTimeZone("Pacific/Auckland");
  const fetchMock = stubFetch(
    Response.json({ olderCount: 3, months: [{ month: "2026-10", count: 2 }] }),
  );
  await expect(
    getArtifactSummary({ since: "2026-10-26T00:00:00.000Z", type: "files" }),
  ).resolves.toEqual({
    olderCount: 3,
    months: [{ month: "2026-10", count: 2 }],
  });
  expect(String(fetchMock.mock.calls[0][0])).toBe(
    "/api/artifacts/summary?since=2026-10-26T00%3A00%3A00.000Z&type=files&tz=Pacific%2FAuckland",
  );
  vi.restoreAllMocks();
});

test("getArtifactSummary leaves an unknown zone to the server's UTC", async () => {
  stubTimeZone(undefined);
  const fetchMock = stubFetch(Response.json({ olderCount: 0, months: [] }));
  await getArtifactSummary({ since: "2026-10-26T00:00:00.000Z", type: "all" });
  expect(String(fetchMock.mock.calls[0][0])).toBe(
    "/api/artifacts/summary?since=2026-10-26T00%3A00%3A00.000Z&type=all",
  );
  vi.restoreAllMocks();
});
