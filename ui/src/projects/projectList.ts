import type { Project } from "../api";

export type ProjectSort = "recent" | "edited" | "created" | "name";

// filterProjects keeps the projects whose name or description holds the query,
// ignoring case; a blank query keeps them all.
export function filterProjects(projects: Project[], query: string): Project[] {
  const term = query.trim().toLowerCase();
  if (term === "") return projects;
  return projects.filter((project) =>
    `${project.name} ${project.description}`.toLowerCase().includes(term),
  );
}

// sortProjects returns a sorted copy: dates newest first, names A to Z.
export function sortProjects(
  projects: Project[],
  sort: ProjectSort,
): Project[] {
  return [...projects].sort((a, b) => {
    if (sort === "name") return a.name.localeCompare(b.name);
    if (sort === "created") return compareDatesDesc(a.createdAt, b.createdAt);
    if (sort === "edited") return compareDatesDesc(a.updatedAt, b.updatedAt);
    return compareDatesDesc(a.lastActivityAt, b.lastActivityAt);
  });
}

function compareDatesDesc(a: string, b: string): number {
  return new Date(b).getTime() - new Date(a).getTime();
}
