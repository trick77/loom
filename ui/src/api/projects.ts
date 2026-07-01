import { AuthExpiredError, expectJSON } from "./http";
import type { Project, ProjectMemory } from "./types";

export async function listProjects(archived?: boolean): Promise<Project[]> {
  const query = archived === undefined ? "" : `?archived=${String(archived)}`;
  const response = await fetch(`/api/projects${query}`);
  return expectJSON<Project[]>(response, "failed to load projects");
}

export async function createProject(input: { name: string; description?: string }): Promise<Project> {
  const response = await fetch("/api/projects", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Project>(response, "failed to create project");
}

export async function updateProject(
  projectId: string,
  input: { name?: string; description?: string },
): Promise<Project> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Project>(response, "failed to update project");
}

export async function setProjectStarred(projectId: string, starred: boolean): Promise<Project> {
  const action = starred ? "star" : "unstar";
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/${action}`, {
    method: "POST",
  });
  return expectJSON<Project>(response, "failed to update project");
}

export async function archiveProject(projectId: string): Promise<void> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/archive`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to archive project");
  }
}

export async function unarchiveProject(projectId: string): Promise<void> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/unarchive`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to unarchive project");
  }
}

export async function deleteProject(projectId: string): Promise<void> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to delete project");
  }
}

export async function getProjectMemory(projectId: string): Promise<ProjectMemory> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/memory`);
  return expectJSON<ProjectMemory>(response, "failed to load project memory");
}

export async function refreshProjectMemory(projectId: string): Promise<ProjectMemory> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/memory:refresh`, {
    method: "POST",
  });
  return expectJSON<ProjectMemory>(response, "failed to refresh project memory");
}

export async function editProjectMemory(
  projectId: string,
  instruction: string,
): Promise<ProjectMemory> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/memory:edit`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ instruction }),
  });
  return expectJSON<ProjectMemory>(response, "failed to edit project memory");
}
