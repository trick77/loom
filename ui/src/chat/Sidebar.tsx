import type { Project, Thread, User } from "../api";
import { UserMenu } from "./UserMenu";
import { Icon } from "./Icon";
import type { RouteState } from "./routing";
import { SidebarPrimaryItem, SidebarProjectItem, SidebarSection } from "./SidebarItems";

export function Sidebar({
  user,
  displayName,
  route,
  showAdmin,
  isMobile,
  sidebarCollapsed,
  railCollapsed,
  mobileSidebarOpen,
  userMenuOpen,
  loadError,
  projectsAvailable,
  starredThreads,
  recentThreads,
  starredProjects,
  unstarredProjects,
  openThreadMenuID,
  onToggleDesktopCollapsed,
  onCloseMobileSidebar,
  onOpenMobileSidebar,
  onToggleUserMenu,
  onCloseUserMenu,
  onOpenSettings,
  onLogout,
  onAdmin,
  onNewChat,
  onChats,
  onArtifacts,
  onProjects,
  onMemory,
  onSelectThread,
  onDeleteThread,
  onRenameThread,
  onAddThreadToProject,
  onStarThread,
  onNavigateProject,
  onStarProject,
  onEditProject,
  onDeleteProject,
  onToggleThreadMenu,
  onCloseThreadMenu,
}: {
  user: User;
  displayName: string;
  route: RouteState;
  showAdmin: boolean;
  isMobile: boolean;
  sidebarCollapsed: boolean;
  railCollapsed: boolean;
  mobileSidebarOpen: boolean;
  userMenuOpen: boolean;
  loadError: string;
  projectsAvailable: boolean;
  starredThreads: Thread[];
  recentThreads: Thread[];
  starredProjects: Project[];
  unstarredProjects: Project[];
  openThreadMenuID: string | null;
  onToggleDesktopCollapsed(): void;
  onCloseMobileSidebar(): void;
  onOpenMobileSidebar(): void;
  onToggleUserMenu(): void;
  onCloseUserMenu(): void;
  onOpenSettings(): void;
  onLogout(): void;
  onAdmin(): void;
  onNewChat(): void;
  onChats(): void;
  onArtifacts(): void;
  onProjects(): void;
  onMemory(): void;
  onSelectThread(threadID: string): void;
  onDeleteThread(thread: Thread): void;
  onRenameThread(thread: Thread): void;
  onAddThreadToProject(thread: Thread): void;
  onStarThread(thread: Thread, starred: boolean, menuKey: string): void;
  onNavigateProject(project: Project): void;
  onStarProject(project: Project, starred: boolean, menuKey: string): void;
  onEditProject(project: Project): void;
  onDeleteProject(project: Project): void;
  onToggleThreadMenu(menuKey: string): void;
  onCloseThreadMenu(): void;
}) {
  const addToProject = projectsAvailable ? onAddThreadToProject : undefined;
  return (
    <>
      <aside
        className={`ui-sidebar-text fixed inset-y-0 left-0 z-50 flex w-[300px] max-w-[85vw] min-h-0 flex-col overflow-hidden border-r border-[#343432] bg-panel pl-0.5 text-[#c7c5bd] transition-transform duration-200 ease-out md:static md:z-auto md:w-auto md:max-w-none md:translate-x-0 ${
          mobileSidebarOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        <div className={`flex h-11 items-center px-3 ${railCollapsed ? "justify-center" : "justify-between"}`}>
          {!railCollapsed && <div className="ui-wordmark font-serif font-medium text-[#f4f0e8]">Loom</div>}
          <div className="flex items-center gap-3 text-[#aaa79e]">
            {!railCollapsed && (
              <button
                type="button"
                aria-label="Search"
                className="grid place-items-center rounded transition-colors hover:text-white"
              >
                <Icon name="search" size="18px" />
              </button>
            )}
            <button
              type="button"
              aria-label={!isMobile && sidebarCollapsed ? "Show sidebar" : "Hide sidebar"}
              aria-expanded={isMobile ? mobileSidebarOpen : !sidebarCollapsed}
              onClick={() => (isMobile ? onCloseMobileSidebar() : onToggleDesktopCollapsed())}
              className="ui-sidebar-btn grid place-items-center rounded transition-colors hover:text-white"
            >
              <Icon name="sidebar" size="18px" className="ui-sidebar-icon" />
            </button>
          </div>
        </div>
        <nav className="ui-sidebar-scroll min-h-0 flex-1 overflow-y-auto px-2 pb-4 pt-2">
          <button
            className={`flex h-7 w-full items-center rounded-md px-1.5 text-left transition-colors hover:bg-[#2a2a28] ${
              railCollapsed ? "justify-center" : "gap-2.5"
            } ${route.view === "new" && !showAdmin ? "bg-[#111110]" : ""}`}
            onClick={onNewChat}
            type="button"
            aria-label="New chat"
          >
            <span className="grid h-[20px] w-[20px] shrink-0 place-items-center rounded-full bg-[hsl(180deg_3%_19%)] text-[hsl(55deg_9%_74%)]">
              <svg className="h-[13px] w-[13px]" viewBox="0 0 24 24" aria-hidden="true" fill="none">
                <path d="M12 4v16M4 12h16" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
              </svg>
            </span>
            {!railCollapsed && <span>New chat</span>}
          </button>
          <SidebarPrimaryItem
            label="Chats"
            icon="chats"
            collapsed={railCollapsed}
            active={route.view === "chats" && !showAdmin}
            onClick={onChats}
          />
          <SidebarPrimaryItem
            label="Artifacts"
            icon="artifacts"
            collapsed={railCollapsed}
            active={route.view === "artifacts" && !showAdmin}
            onClick={onArtifacts}
          />
          <SidebarPrimaryItem
            label="Projects"
            icon="projects"
            collapsed={railCollapsed}
            active={(route.view === "projects" || route.view === "project") && !showAdmin}
            onClick={onProjects}
          />
          <SidebarPrimaryItem
            label="Memories"
            icon="memory"
            collapsed={railCollapsed}
            active={route.view === "memory" && !showAdmin}
            onClick={onMemory}
          />
          {!railCollapsed && (
            <>
              {loadError !== "" && (
                <div className="ui-meta-text mx-1.5 mt-3 rounded-md border border-accent px-2 py-2 text-accent">
                  {loadError}
                </div>
              )}
              <SidebarSection
                title="Starred"
                threads={starredThreads}
                activeThreadID={route.view === "chat" ? route.threadID : null}
                openThreadMenuID={openThreadMenuID}
                onSelect={onSelectThread}
                onDelete={onDeleteThread}
                onRename={onRenameThread}
                onAddToProject={addToProject}
                onStarChange={onStarThread}
                onToggleMenu={onToggleThreadMenu}
                onCloseMenu={onCloseThreadMenu}
                leading={starredProjects.map((project) => (
                  <SidebarProjectItem
                    key={project.id}
                    menuKey={`StarredProject:${project.id}`}
                    project={project}
                    active={route.view === "project" && route.projectID === project.id}
                    menuOpen={openThreadMenuID === `StarredProject:${project.id}`}
                    onNavigate={onNavigateProject}
                    onStarChange={onStarProject}
                    onEdit={onEditProject}
                    onDelete={onDeleteProject}
                    onToggleMenu={onToggleThreadMenu}
                    onCloseMenu={onCloseThreadMenu}
                  />
                ))}
              />
              <section className="mt-5">
                <div className="ui-meta-text mb-2 px-1.5 text-[#97958c]">
                  <span>Projects</span>
                </div>
                <div className="space-y-1.5">
                  {unstarredProjects.map((project) => (
                    <SidebarProjectItem
                      key={project.id}
                      menuKey={`SidebarProject:${project.id}`}
                      project={project}
                      active={route.view === "project" && route.projectID === project.id}
                      menuOpen={openThreadMenuID === `SidebarProject:${project.id}`}
                      onNavigate={onNavigateProject}
                      onStarChange={onStarProject}
                      onEdit={onEditProject}
                      onDelete={onDeleteProject}
                      onToggleMenu={onToggleThreadMenu}
                      onCloseMenu={onCloseThreadMenu}
                    />
                  ))}
                </div>
              </section>
              <SidebarSection
                title="Recents"
                threads={recentThreads}
                activeThreadID={route.view === "chat" ? route.threadID : null}
                openThreadMenuID={openThreadMenuID}
                onSelect={onSelectThread}
                onDelete={onDeleteThread}
                onRename={onRenameThread}
                onAddToProject={addToProject}
                onStarChange={onStarThread}
                onToggleMenu={onToggleThreadMenu}
                onCloseMenu={onCloseThreadMenu}
              />
              {user.role === "admin" && (
                <button
                  className="mt-3 flex h-7 w-full items-center rounded-md px-1.5 text-left transition-colors hover:bg-[#2a2a28]"
                  onClick={onAdmin}
                  type="button"
                >
                  Admin
                </button>
              )}
            </>
          )}
        </nav>
        <div className="relative border-t border-[#343432] px-3 py-3">
          {/* The whole row (avatar circle + name) is the menu trigger, so clicking
              the round user circle opens the account menu too - and it works while
              the rail is collapsed, where only the avatar is shown. */}
          <button
            className={`flex w-full items-center rounded-md ${
              railCollapsed ? "justify-center p-1" : "gap-3 px-1.5 py-1"
            }`}
            type="button"
            aria-label="Account menu"
            aria-haspopup="menu"
            aria-expanded={userMenuOpen}
            onClick={onToggleUserMenu}
          >
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-[#dedbd0] text-xs font-semibold text-[#1d1d1b]">
              {initialsFor(displayName)}
            </div>
            {!railCollapsed && (
              <div className="min-w-0 flex-1 text-left">
                <div className="truncate text-[#f4f0e8]">{displayName}</div>
                <div className="truncate font-normal text-[#8f8b82]">{roleLabel(user.role)}</div>
              </div>
            )}
          </button>
          {userMenuOpen && (
            <>
              <div className="fixed inset-0 z-20" aria-hidden="true" onClick={onCloseUserMenu} />
              <UserMenu
                className="bottom-full left-3 mb-2"
                onClose={onCloseUserMenu}
                onSettings={onOpenSettings}
                onLogout={onLogout}
              />
            </>
          )}
        </div>
      </aside>
      {mobileSidebarOpen && (
        <div className="fixed inset-0 z-40 bg-black/50 md:hidden" onClick={onCloseMobileSidebar} aria-hidden="true" />
      )}
    </>
  );
}

function roleLabel(role: User["role"]): string {
  return role === "admin" ? "Admin" : "User";
}

function initialsFor(name: string): string {
  const trimmed = name.trim();
  if (trimmed === "") return "S";
  return trimmed
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");
}
