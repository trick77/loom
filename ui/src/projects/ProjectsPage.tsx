import { useEffect, useMemo, useState } from "react";

import type { Project } from "../api";
import { Icon } from "../chat/Icon";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { ProjectActionsMenu } from "./ProjectActionsMenu";

type ProjectSort = "recent" | "edited" | "created";

const SORT_LABELS: Record<ProjectSort, string> = {
  recent: "Recent activity",
  edited: "Last edited",
  created: "Date created",
};

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
  const [sort, setSort] = useState<ProjectSort>("recent");
  const [sortOpen, setSortOpen] = useState(false);
  const filtered = useMemo(() => {
    const term = query.trim().toLowerCase();
    const matches = term === "" ? projects : projects.filter((project) =>
      `${project.name} ${project.description}`.toLowerCase().includes(term),
    );
    return [...matches].sort((a, b) => {
      if (sort === "created") return compareDatesDesc(a.createdAt, b.createdAt);
      return compareDatesDesc(a.updatedAt, b.updatedAt);
    });
  }, [projects, query, sort]);

  useEffect(() => {
    if (openMenuID === null) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Element)) return;
      if (target.closest("[data-project-card-menu-root]") !== null) return;
      setOpenMenuID(null);
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [openMenuID]);

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[860px] px-4 pb-16 pt-10 md:px-6">
        <header className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <SidebarOpenButton onClick={onOpenSidebar} />
            <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">Projects</h1>
          </div>
          <div className="flex items-center gap-2.5">
            <div className="relative">
              <button
                aria-expanded={sortOpen}
                className="ui-control-text flex items-center gap-1.5 rounded-lg bg-[#3a3a37] px-3 py-1.5 text-[#d5d2c9] hover:bg-[#4a4a46]"
                type="button"
                onClick={() => setSortOpen((value) => !value)}
              >
                Sort by <span className="font-semibold text-white">{SORT_LABELS[sort]}</span>
                <Icon name="chevronDown" size="14px" />
              </button>
              {sortOpen && (
                <div
                  aria-label="Project sort options"
                  className="ui-sidebar-text absolute right-0 top-full z-20 mt-1 w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
                  role="menu"
                >
                  {(["recent", "edited", "created"] as const).map((option) => (
                    <button
                      key={option}
                      aria-checked={sort === option}
                      className="flex h-[34px] w-full items-center justify-between gap-2.5 px-3 text-left text-[#f3f0e8] hover:bg-[#2a2a28]"
                      role="menuitemradio"
                      type="button"
                      onClick={() => {
                        setSort(option);
                        setSortOpen(false);
                      }}
                    >
                      {SORT_LABELS[option]}
                      {sort === option && <Icon name="check" size="16px" className="text-[#4f8cff]" />}
                    </button>
                  ))}
                </div>
              )}
            </div>
            <button
              className="ui-control-text rounded-lg bg-white px-3 py-1.5 font-medium text-[#1d1d1b]"
              type="button"
              onClick={onCreateProject}
            >
              New project
            </button>
          </div>
        </header>
        <div className="relative mt-6">
          <Icon
            name="search"
            size="18px"
            className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[#807d74]"
          />
          <input
            autoFocus
            className="ui-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
            placeholder="Search projects..."
            aria-label="Search projects"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        {loadError !== "" && (
          <div className="ui-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent">
            {loadError}
          </div>
        )}
        {filtered.length === 0 ? (
          <div className="py-10 text-center text-[#807d74]">
            {query.trim() === "" ? "No projects yet." : "No projects match your search."}
          </div>
        ) : (
        <div className="mt-7 grid gap-6 md:grid-cols-2">
          {filtered.map((project) => (
            <article
              key={project.id}
              className="relative min-h-[160px] cursor-pointer rounded-[10px] border border-[#343432] bg-[#181817] p-4 transition-colors hover:bg-[#2a2a28]"
              onClick={() => onOpenProject(project)}
            >
              <button
                className="block max-w-[calc(100%-42px)] text-left text-sm font-semibold text-[#f4f0e8]"
                type="button"
                onClick={(event) => {
                  event.stopPropagation();
                  onOpenProject(project);
                }}
              >
                {project.name}
              </button>
              {project.description !== "" && (
                <p className="mt-5 line-clamp-3 text-sm leading-5 text-[#c7c5bd]">{project.description}</p>
              )}
              <p className="absolute bottom-4 left-4 text-sm text-[#8f8b82]">
                Updated {formatProjectDate(project.updatedAt)}
              </p>
              <div
                className="absolute right-3 top-3"
                data-project-card-menu-root
                onClick={(event) => event.stopPropagation()}
              >
                <button
                  aria-expanded={openMenuID === project.id}
                  aria-label={`Open project actions for ${project.name}`}
                  className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#2a2a28]"
                  type="button"
                  onClick={(event) => {
                    event.stopPropagation();
                    setOpenMenuID((current) => (current === project.id ? null : project.id));
                  }}
                >
                  <Icon name="moreVertical" size="18px" />
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
        )}
      </div>
    </div>
  );
}

function compareDatesDesc(a: string, b: string): number {
  return new Date(b).getTime() - new Date(a).getTime();
}

function formatProjectDate(value: string): string {
  return new Date(value).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
