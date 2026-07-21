import { afterEach, describe, expect, test, vi } from "vitest";
import { AuthExpiredError } from "./http";
import {
  archiveProject,
  createProject,
  deleteProject,
  editProjectMemory,
  getProjectMemory,
  listProjects,
  refreshProjectMemory,
  setProjectStarred,
  unarchiveProject,
  updateProject,
} from "./projects";

afterEach(() => {
  vi.unstubAllGlobals();
});

const project = {
  id: "p1",
  name: "Roadmap",
  description: "Planning",
  starred: false,
  archived: false,
  createdAt: "2026-06-01T00:00:00Z",
  updatedAt: "2026-06-01T00:00:00Z",
};

const memory = {
  content: "remembered things",
  updatedAt: "2026-06-01T00:00:00Z",
};

function stubFetch(response: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("listProjects", () => {
  test("omits the archived query when unspecified", async () => {
    const fetchMock = stubFetch(Response.json([project]));

    await expect(listProjects()).resolves.toEqual([project]);
    expect(fetchMock).toHaveBeenCalledWith("/api/projects");
  });

  test.each([true, false])(
    "sends archived=%s when specified",
    async (archived) => {
      const fetchMock = stubFetch(Response.json([]));

      await listProjects(archived);

      expect(fetchMock).toHaveBeenCalledWith(
        `/api/projects?archived=${String(archived)}`,
      );
    },
  );

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(listProjects()).rejects.toThrow("failed to load projects");
  });
});

describe("createProject", () => {
  test("posts the project payload", async () => {
    const fetchMock = stubFetch(Response.json(project));

    await expect(
      createProject({ name: "Roadmap", description: "Planning" }),
    ).resolves.toEqual(project);
    expect(fetchMock).toHaveBeenCalledWith("/api/projects", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "Roadmap", description: "Planning" }),
    });
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 400 }));

    await expect(createProject({ name: "Roadmap" })).rejects.toThrow(
      "failed to create project",
    );
  });
});

describe("updateProject", () => {
  test("patches the project and encodes the id", async () => {
    const fetchMock = stubFetch(Response.json(project));

    await expect(updateProject("p 1/x", { name: "Renamed" })).resolves.toEqual(
      project,
    );
    expect(fetchMock).toHaveBeenCalledWith("/api/projects/p%201%2Fx", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "Renamed" }),
    });
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(
      updateProject("p1", { name: "Renamed" }),
    ).rejects.toBeInstanceOf(AuthExpiredError);
  });
});

describe("setProjectStarred", () => {
  test.each([
    [true, "star"],
    [false, "unstar"],
  ])("posts to the %s endpoint", async (starred, action) => {
    const fetchMock = stubFetch(Response.json({ ...project, starred }));

    await expect(setProjectStarred("p1", starred)).resolves.toEqual({
      ...project,
      starred,
    });
    expect(fetchMock).toHaveBeenCalledWith(`/api/projects/p1/${action}`, {
      method: "POST",
    });
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(setProjectStarred("p1", true)).rejects.toThrow(
      "failed to update project",
    );
  });
});

describe.each([
  ["archiveProject", archiveProject, "archive", "failed to archive project"],
  [
    "unarchiveProject",
    unarchiveProject,
    "unarchive",
    "failed to unarchive project",
  ],
] as const)("%s", (_name, call, action, errorMessage) => {
  test("posts and resolves with no body", async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await expect(call("p1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(`/api/projects/p1/${action}`, {
      method: "POST",
    });
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(call("p1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(call("p1")).rejects.toThrow(errorMessage);
  });
});

describe("deleteProject", () => {
  test("deletes and resolves with no body", async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await expect(deleteProject("p1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith("/api/projects/p1", {
      method: "DELETE",
    });
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(deleteProject("p1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 409 }));

    await expect(deleteProject("p1")).rejects.toThrow(
      "failed to delete project",
    );
  });
});

describe("project memory", () => {
  test("getProjectMemory reads the memory endpoint", async () => {
    const fetchMock = stubFetch(Response.json(memory));

    await expect(getProjectMemory("p1")).resolves.toEqual(memory);
    expect(fetchMock).toHaveBeenCalledWith("/api/projects/p1/memory");
  });

  test("getProjectMemory throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(getProjectMemory("p1")).rejects.toThrow(
      "failed to load project memory",
    );
  });

  test("refreshProjectMemory posts to the refresh endpoint", async () => {
    const fetchMock = stubFetch(Response.json(memory));

    await expect(refreshProjectMemory("p1")).resolves.toEqual(memory);
    expect(fetchMock).toHaveBeenCalledWith("/api/projects/p1/memory:refresh", {
      method: "POST",
    });
  });

  test("refreshProjectMemory throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 503 }));

    await expect(refreshProjectMemory("p1")).rejects.toThrow(
      "failed to refresh project memory",
    );
  });

  test("editProjectMemory sends the instruction", async () => {
    const fetchMock = stubFetch(Response.json(memory));

    await expect(
      editProjectMemory("p1", "forget the launch date"),
    ).resolves.toEqual(memory);
    expect(fetchMock).toHaveBeenCalledWith("/api/projects/p1/memory:edit", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ instruction: "forget the launch date" }),
    });
  });

  test("editProjectMemory throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(editProjectMemory("p1", "forget")).rejects.toThrow(
      "failed to edit project memory",
    );
  });
});
