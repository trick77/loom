import { useMemo, useState } from "react";

import type { Project } from "../api";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { ProjectActionsMenu } from "./ProjectActionsMenu";

export function ProjectsPage({
  projects,
  loadError,
  onOpenSidebar,
  onCreateProject,
  onOpenProject,
  onEditProject,
  onArchiveProject,
  onDeleteProject,
}: {
  projects: Project[];
  loadError: string;
  onOpenSidebar(): void;
  onCreateProject(): void;
  onOpenProject(project: Project): void;
  onEditProject(project: Project): void;
  onArchiveProject(project: Project): void;
  onDeleteProject(project: Project): void;
}) {
  const [query, setQuery] = useState("");
  const [openMenuID, setOpenMenuID] = useState<string | null>(null);
  const filtered = useMemo(() => {
    const term = query.trim().toLowerCase();
    if (term === "") return projects;
    return projects.filter((project) =>
      `${project.name} ${project.description}`.toLowerCase().includes(term),
    );
  }, [projects, query]);

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[860px] px-4 pb-16 pt-10 md:px-6">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <SidebarOpenButton onClick={onOpenSidebar} />
            <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">Projects</h1>
          </div>
          <div className="flex items-center gap-2.5">
            <button className="slopr-control-text rounded-lg bg-[#3a3a37] px-3 py-2 text-[#d5d2c9]" type="button">
              Sort by <span className="font-semibold text-white">Recent activity</span>
            </button>
            <button
              className="slopr-control-text rounded-lg bg-white px-3 py-2 font-medium text-[#1d1d1b]"
              type="button"
              onClick={onCreateProject}
            >
              New project
            </button>
          </div>
        </header>
        <input
          className="slopr-composer-text mt-6 h-10 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] px-4 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          placeholder="Search projects..."
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        {loadError !== "" && (
          <div className="slopr-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent">
            {loadError}
          </div>
        )}
        <div className="mt-7 grid gap-6 md:grid-cols-2">
          {filtered.map((project) => (
            <article
              key={project.id}
              className="relative min-h-[160px] rounded-[10px] border border-[#343432] bg-[#181817] p-4"
            >
              <button
                className="block max-w-[calc(100%-42px)] text-left text-sm font-semibold text-[#f4f0e8]"
                type="button"
                onClick={() => onOpenProject(project)}
              >
                {project.name}
              </button>
              {project.description !== "" && (
                <p className="mt-5 line-clamp-3 text-sm leading-5 text-[#c7c5bd]">{project.description}</p>
              )}
              <p className="absolute bottom-4 left-4 text-xs text-[#8f8b82]">
                Updated {formatProjectDate(project.updatedAt)}
              </p>
              <div className="absolute right-3 top-3">
                <button
                  aria-expanded={openMenuID === project.id}
                  aria-label={`Open project actions for ${project.name}`}
                  className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#2a2a28]"
                  type="button"
                  onClick={() => setOpenMenuID((current) => (current === project.id ? null : project.id))}
                >
                  ...
                </button>
                {openMenuID === project.id && (
                  <ProjectActionsMenu
                    project={project}
                    onEdit={onEditProject}
                    onArchive={onArchiveProject}
                    onDelete={onDeleteProject}
                  />
                )}
              </div>
            </article>
          ))}
        </div>
      </div>
    </div>
  );
}

function formatProjectDate(value: string): string {
  return new Date(value).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
