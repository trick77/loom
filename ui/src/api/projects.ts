import { request, requestJSON } from "./http";
import type { Project, ProjectMemory } from "./types";

const projectUrl = (projectId: string) =>
  `/api/projects/${encodeURIComponent(projectId)}`;

export async function listProjects(archived?: boolean): Promise<Project[]> {
  const query = archived === undefined ? "" : `?archived=${String(archived)}`;
  return requestJSON(`/api/projects${query}`, "failed to load projects");
}

export async function createProject(input: {
  name: string;
  description?: string;
}): Promise<Project> {
  return requestJSON("/api/projects", "failed to create project", {
    method: "POST",
    json: input,
  });
}

export async function updateProject(
  projectId: string,
  input: { name?: string; description?: string },
): Promise<Project> {
  return requestJSON(projectUrl(projectId), "failed to update project", {
    method: "PATCH",
    json: input,
  });
}

export async function setProjectStarred(
  projectId: string,
  starred: boolean,
): Promise<Project> {
  const action = starred ? "star" : "unstar";
  return requestJSON(
    `${projectUrl(projectId)}/${action}`,
    "failed to update project",
    { method: "POST" },
  );
}

export async function archiveProject(projectId: string): Promise<void> {
  await request(
    `${projectUrl(projectId)}/archive`,
    "failed to archive project",
    {
      method: "POST",
    },
  );
}

export async function unarchiveProject(projectId: string): Promise<void> {
  await request(
    `${projectUrl(projectId)}/unarchive`,
    "failed to unarchive project",
    { method: "POST" },
  );
}

export async function deleteProject(projectId: string): Promise<void> {
  await request(projectUrl(projectId), "failed to delete project", {
    method: "DELETE",
  });
}

export async function getProjectMemory(
  projectId: string,
): Promise<ProjectMemory> {
  return requestJSON(
    `${projectUrl(projectId)}/memory`,
    "failed to load project memory",
  );
}

export async function editProjectMemory(
  projectId: string,
  instruction: string,
): Promise<ProjectMemory> {
  return requestJSON(
    `${projectUrl(projectId)}/memory:edit`,
    "failed to edit project memory",
    { method: "POST", json: { instruction } },
  );
}
