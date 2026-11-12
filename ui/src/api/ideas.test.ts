import { afterEach, expect, test, vi } from "vitest";

import { UserFacingError } from "./http";
import { createIdea, listIdeas, restoreIdea, updateIdea } from "./ideas";
import { browserTimezone } from "./schedules";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(response: Response) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

test("listIdeas sends the zone, so 'today' is the user's day", async () => {
  const fetchMock = stubFetch(Response.json({ items: [] }));
  await expect(listIdeas("done")).resolves.toEqual([]);
  const [url, init] = fetchMock.mock.calls[0];
  expect(url).toBe("/api/ideas?status=done");
  expect(new Headers(init.headers).get("X-Loom-Timezone")).toBe(
    browserTimezone(),
  );
});

test("a null date goes over the wire as null, an absent one stays absent", async () => {
  const fetchMock = stubFetch(Response.json({ id: "i1" }));
  await updateIdea("i 1", { remindOn: null, title: "x" });
  const [url, init] = fetchMock.mock.calls[0];
  expect(url).toBe("/api/ideas/i%201");
  expect(init.method).toBe("PATCH");
  expect(JSON.parse(init.body)).toEqual({ remindOn: null, title: "x" });
});

test("a 400 carries the server's text to the form", async () => {
  stubFetch(Response.json({ error: "reason is required" }, { status: 400 }));
  await expect(createIdea({ reason: " " })).rejects.toEqual(
    new UserFacingError("reason is required"),
  );
});

test("restore posts without a body", async () => {
  const fetchMock = stubFetch(Response.json({ id: "i1" }));
  await restoreIdea("i1");
  const [url, init] = fetchMock.mock.calls[0];
  expect(url).toBe("/api/ideas/i1/restore");
  expect(init.method).toBe("POST");
  expect(init.body).toBeUndefined();
});
