import "@testing-library/jest-dom/vitest";
import { useState } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import * as api from "../api";
import type { Project, Thread } from "../api";
import { ICONS } from "../chat/Icon";
import { ProjectDialog } from "./ProjectDialog";
import { ProjectDetailPage } from "./ProjectDetailPage";
import { ProjectsPage } from "./ProjectsPage";

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return { ...actual, listProjects: vi.fn() };
});

const projects: Project[] = [
  {
    id: "p1",
    name: "Research",
    description: "Paper notes",
    starred: false,
    // The backend serializes archivedAt as null (not omitted) for active
    // projects; the badge predicate must treat null as "not archived".
    archivedAt: null,
    createdAt: "2026-06-10T00:00:00Z",
    // updatedAt is the project-record edit time; deliberately distinct from
    // lastActivityAt so the card's "Updated … ago" proves it reads activity.
    updatedAt: "2026-06-10T00:00:00Z",
    lastActivityAt: new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString(),
  },
];

// Relative to "now" so the row's relative-time label ("… ago") is deterministic
// regardless of when the suite runs (a fixed date crosses the "yesterday"
// boundary as the day progresses).
const recentIso = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString();

const threads: Thread[] = [
  {
    id: "t1",
    projectId: "p1",
    title: "Literature review",
    starred: false,
    createdAt: "2026-06-10T00:00:00Z",
    updatedAt: "2026-06-10T12:00:00Z",
    lastMessageAt: recentIso,
  },
];

test("ProjectsPage renders projects without reference-only controls", () => {
  render(
    <ProjectsPage
      projects={projects}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  expect(screen.getByRole("heading", { name: "Projects" })).toBeInTheDocument();
  expect(screen.getByPlaceholderText("Search projects...")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "New project" })).toBeInTheDocument();
  expect(screen.getByText("Research")).toBeInTheDocument();
  expect(screen.getByText("Paper notes")).toBeInTheDocument();
  expect(screen.getByText("Updated 3 hours ago")).toHaveClass("text-sm");
  expect(screen.getByText("Updated 3 hours ago")).not.toHaveClass("text-xs");
  expect(screen.getByRole("button", { name: "Sort by Recent activity" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Open project actions for Research" })).toHaveTextContent(
    ICONS.moreVertical,
  );
  expect(screen.queryByText("Example project")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Share" })).not.toBeInTheDocument();
});

test("ProjectsPage shows archived projects only under the Archived tab", async () => {
  const archivedProject: Project = {
    id: "p2",
    name: "Old initiative",
    description: "Wrapped up",
    starred: false,
    archivedAt: "2026-06-01T00:00:00Z",
    createdAt: "2026-05-01T00:00:00Z",
    updatedAt: "2026-06-01T00:00:00Z",
    lastActivityAt: "2026-06-01T00:00:00Z",
  };
  vi.mocked(api.listProjects).mockResolvedValue([archivedProject]);

  render(
    <ProjectsPage
      projects={projects}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  // Active tab (C2/C8): the active project shows, the archived one does not,
  // and an active project (archivedAt: null) carries no badge (C7 regression).
  expect(screen.getByText("Research")).toBeInTheDocument();
  expect(screen.queryByText("Old initiative")).not.toBeInTheDocument();
  expect(screen.queryByRole("img", { name: "Archived" })).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole("tab", { name: "Archived" }));

  // Archived tab is tab-local: only the archived project, with its name badge (C7).
  expect(await screen.findByText("Old initiative")).toBeInTheDocument();
  expect(screen.queryByText("Research")).not.toBeInTheDocument();
  expect(api.listProjects).toHaveBeenCalledWith(true);
  const card = screen.getByText("Old initiative").closest("article");
  expect(within(card!).getByRole("img", { name: "Archived" })).toBeInTheDocument();

  // The archived card's action menu offers Unarchive instead of Archive.
  fireEvent.click(screen.getByRole("button", { name: "Open project actions for Old initiative" }));
  const menu = screen.getByRole("menu", { name: "Project actions" });
  expect(within(menu).getByRole("menuitem", { name: "Unarchive" })).toBeInTheDocument();
  expect(within(menu).queryByRole("menuitem", { name: "Archive" })).not.toBeInTheDocument();
});

test("ProjectsPage opens the sort dropdown with project sort options", () => {
  render(
    <ProjectsPage
      projects={projects}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Sort by Recent activity" }));

  const menu = screen.getByRole("menu", { name: "Project sort options" });
  expect(within(menu).getByRole("menuitemradio", { name: "Recent activity" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  expect(within(menu).getByRole("menuitemradio", { name: "Last edited" })).toBeInTheDocument();
  expect(within(menu).getByRole("menuitemradio", { name: "Date created" })).toBeInTheDocument();
});

test("ProjectsPage sort options each produce a distinct ordering", () => {
  // Three projects whose three timestamps disagree, so a correct mapping yields
  // a different order per option (the regression: recent and edited were identical).
  const ordering: Project[] = [
    {
      id: "a",
      name: "Alpha",
      description: "",
      starred: false,
      archivedAt: null,
      createdAt: "2026-03-01T00:00:00Z", // newest created
      updatedAt: "2026-01-01T00:00:00Z", // oldest edited
      lastActivityAt: "2026-02-02T00:00:00Z",
    },
    {
      id: "b",
      name: "Bravo",
      description: "",
      starred: false,
      archivedAt: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-03-01T00:00:00Z", // newest edited
      lastActivityAt: "2026-02-01T00:00:00Z", // oldest activity
    },
    {
      id: "c",
      name: "Charlie",
      description: "",
      starred: false,
      archivedAt: null,
      createdAt: "2026-02-01T00:00:00Z",
      updatedAt: "2026-02-02T00:00:00Z",
      lastActivityAt: "2026-03-01T00:00:00Z", // newest activity
    },
  ];

  render(
    <ProjectsPage
      projects={ordering}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  const names = () =>
    screen.getAllByRole("article").map((card) => within(card).getByText(/Alpha|Bravo|Charlie/).textContent);

  // Default sort is Recent activity → by lastActivityAt desc.
  expect(names()).toEqual(["Charlie", "Alpha", "Bravo"]);

  fireEvent.click(screen.getByRole("button", { name: /^Sort by/ }));
  fireEvent.click(screen.getByRole("menuitemradio", { name: "Last edited" }));
  expect(names()).toEqual(["Bravo", "Charlie", "Alpha"]); // by updatedAt desc

  fireEvent.click(screen.getByRole("button", { name: /^Sort by/ }));
  fireEvent.click(screen.getByRole("menuitemradio", { name: "Date created" }));
  expect(names()).toEqual(["Alpha", "Charlie", "Bravo"]); // by createdAt desc
});

test("ProjectsPage opens a project from anywhere on the card body", () => {
  const onOpenProject = vi.fn();
  render(
    <ProjectsPage
      projects={projects}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={onOpenProject}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  const card = screen.getByText("Paper notes").closest("article");

  expect(card).toHaveClass("hover:bg-[#2a2a28]");
  fireEvent.click(screen.getByText("Paper notes"));
  expect(onOpenProject).toHaveBeenCalledWith(projects[0]);
});

test("ProjectDetailPage renders project chats and project chat menu", () => {
  render(
    <ProjectDetailPage
      project={projects[0]}
      threads={threads}
      draft=""
      sendError=""
      isSending={false}
      openThreadMenuID={null}
      onBack={vi.fn()}
      onDraftChange={vi.fn()}
      onSend={vi.fn()}
      onStop={vi.fn()}
      onOpenThread={vi.fn()}
      onRenameThread={vi.fn()}
      onDeleteThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
      onToggleStar={vi.fn()}
      onOpenSidebar={vi.fn()}
    />,
  );

  expect(screen.getByRole("button", { name: "All projects" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Research" })).toBeInTheDocument();
  expect(screen.getByText("Paper notes")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /Literature review/ })).toBeInTheDocument();
  expect(screen.queryByText("Example project")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Share" })).not.toBeInTheDocument();
});

test("ProjectDetailPage offers Unarchive for an archived project", () => {
  const archivedProject: Project = { ...projects[0], archivedAt: "2026-06-01T00:00:00Z" };
  render(
    <ProjectDetailPage
      project={archivedProject}
      threads={threads}
      draft=""
      sendError=""
      isSending={false}
      openThreadMenuID={`Project:${archivedProject.id}`}
      onBack={vi.fn()}
      onDraftChange={vi.fn()}
      onSend={vi.fn()}
      onStop={vi.fn()}
      onOpenThread={vi.fn()}
      onRenameThread={vi.fn()}
      onDeleteThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
      onToggleStar={vi.fn()}
      onOpenSidebar={vi.fn()}
    />,
  );

  // Its chats stay visible (C5) and the menu offers Unarchive, not Archive.
  expect(screen.getByRole("button", { name: /Literature review/ })).toBeInTheDocument();
  const menu = screen.getByRole("menu", { name: "Project actions" });
  expect(within(menu).getByRole("menuitem", { name: "Unarchive" })).toBeInTheDocument();
  expect(within(menu).queryByRole("menuitem", { name: "Archive" })).not.toBeInTheDocument();
});

test("ProjectDetailPage renders project chats with the shared chats-list row", () => {
  render(
    <ProjectDetailPage
      project={projects[0]}
      threads={threads}
      draft=""
      sendError=""
      isSending={false}
      openThreadMenuID={null}
      onBack={vi.fn()}
      onDraftChange={vi.fn()}
      onSend={vi.fn()}
      onStop={vi.fn()}
      onOpenThread={vi.fn()}
      onRenameThread={vi.fn()}
      onDeleteThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
      onToggleStar={vi.fn()}
      onOpenSidebar={vi.fn()}
    />,
  );

  const rowButton = screen.getByRole("button", { name: /Literature review/ });
  const rowSurface = rowButton.closest("div");
  const row = rowButton.closest("li");

  expect(rowButton).toHaveTextContent("ago");
  expect(row).toHaveClass("border-b");
  expect(rowSurface).toHaveClass("h-[49px]");
  expect(rowSurface).toHaveClass("rounded-xl");
  expect(rowSurface).not.toHaveClass("border-b");
  expect(rowSurface).toHaveClass("px-3");
  expect(rowSurface).not.toHaveClass("px-1.5");
  expect(rowSurface).toHaveClass("hover:bg-[#2a2a28]");
  expect(rowSurface).not.toBeNull();
  const timeLabel = rowSurface?.querySelector("[data-thread-row-time]");
  expect(timeLabel).toHaveClass("ml-auto");
  expect(timeLabel).toHaveClass("group-hover:hidden");
  expect(timeLabel).toHaveClass("[@media(hover:none)]:hidden");
  const actionButton = within(rowSurface!).getByRole("button", { name: "Open thread actions" });
  expect(actionButton).toHaveClass("absolute");
  expect(actionButton).toHaveClass("right-3");
  expect(actionButton).toHaveClass("[@media(hover:none)]:visible");
});

test("ProjectDetailPage uses the same composer surface as new chat", () => {
  render(
    <ProjectDetailPage
      project={projects[0]}
      threads={threads}
      draft=""
      sendError=""
      isSending={false}
      openThreadMenuID={null}
      onBack={vi.fn()}
      onDraftChange={vi.fn()}
      onSend={vi.fn()}
      onStop={vi.fn()}
      onOpenThread={vi.fn()}
      onRenameThread={vi.fn()}
      onDeleteThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
      onToggleStar={vi.fn()}
      onOpenSidebar={vi.fn()}
    />,
  );

  const composerText = screen.getByPlaceholderText("How can I help you today?");
  const sendButton = screen.getByRole("button", { name: "Send message" });

  expect(composerText.closest("form")).toHaveClass("ui-composer");
  expect(composerText).toHaveClass("ui-composer-text");
  expect(screen.getByRole("button", { name: "Add attachment" })).toBeInTheDocument();
  expect(sendButton).toHaveClass("ui-composer-send");
});

test("project action menus expose edit archive delete", () => {
  render(
    <ProjectsPage
      projects={projects}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Open project actions for Research" }));
  const menu = screen.getByRole("menu", { name: "Project actions" });
  expect(within(menu).getByRole("menuitem", { name: "Edit details" })).toBeInTheDocument();
  expect(within(menu).getByRole("menuitem", { name: "Archive" })).toBeInTheDocument();
  expect(within(menu).getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
});

test("project action menu icons align with the first line of wrapping action text", () => {
  render(
    <ProjectsPage
      projects={projects}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Open project actions for Research" }));
  const item = within(screen.getByRole("menu", { name: "Project actions" })).getByRole("menuitem", {
    name: "Edit details",
  });
  const icon = item.querySelector("[aria-hidden='true']");

  expect(item).toHaveClass("min-h-[30px]");
  expect(item).toHaveClass("items-start");
  expect(item).not.toHaveClass("items-center");
  expect(icon).toHaveClass("h-[21px]");
});

test("ProjectsPage closes project action menu when clicking outside", () => {
  render(
    <ProjectsPage
      projects={projects}
      loadError=""
      onOpenSidebar={vi.fn()}
      onCreateProject={vi.fn()}
      onOpenProject={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Open project actions for Research" }));
  expect(screen.getByRole("menu", { name: "Project actions" })).toBeInTheDocument();

  fireEvent.pointerDown(document.body);

  expect(screen.queryByRole("menu", { name: "Project actions" })).not.toBeInTheDocument();
});

test("project action triggers use vertical overflow icons", () => {
  render(
    <ProjectDetailPage
      project={projects[0]}
      threads={threads}
      draft=""
      sendError=""
      isSending={false}
      openThreadMenuID={null}
      onBack={vi.fn()}
      onDraftChange={vi.fn()}
      onSend={vi.fn()}
      onStop={vi.fn()}
      onOpenThread={vi.fn()}
      onRenameThread={vi.fn()}
      onDeleteThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onUnarchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
      onToggleStar={vi.fn()}
      onOpenSidebar={vi.fn()}
    />,
  );

  expect(screen.getByRole("button", { name: "Open project actions" })).toHaveTextContent(ICONS.moreVertical);
  expect(screen.getByRole("button", { name: "Open thread actions" })).toHaveTextContent(ICONS.moreVertical);
});

test("ProjectDialog uses the verified close icon glyph", () => {
  render(
    <ProjectDialog
      project={null}
      error=""
      disabled={false}
      onCancel={vi.fn()}
      onSubmit={vi.fn()}
    />,
  );

  expect(screen.getByRole("button", { name: "Close" })).toHaveTextContent(String.fromCodePoint(0xe10f));
  expect(ICONS.close).toBe(String.fromCodePoint(0xe10f));
});

test("ProjectDetailPage thread menu items remain clickable after pointerdown", () => {
  const onRemoveFromProject = vi.fn();

  function Harness() {
    const [openMenuID, setOpenMenuID] = useState<string | null>(threads[0].id);
    return (
      <ProjectDetailPage
        project={projects[0]}
        threads={threads}
        draft=""
        sendError=""
        isSending={false}
        openThreadMenuID={openMenuID}
        onBack={vi.fn()}
        onDraftChange={vi.fn()}
        onSend={vi.fn()}
        onStop={vi.fn()}
        onOpenThread={vi.fn()}
        onRenameThread={vi.fn()}
        onDeleteThread={vi.fn()}
        onStarThread={vi.fn()}
        onRemoveFromProject={onRemoveFromProject}
        onToggleThreadMenu={(menuKey) => setOpenMenuID((current) => (current === menuKey ? null : menuKey))}
        onCloseThreadMenu={() => setOpenMenuID(null)}
        onEditProject={vi.fn()}
        onArchiveProject={vi.fn()}
        onUnarchiveProject={vi.fn()}
        onDeleteProject={vi.fn()}
        onToggleStar={vi.fn()}
        onOpenSidebar={vi.fn()}
      />
    );
  }

  render(<Harness />);

  const removeItem = screen.getByRole("menuitem", { name: "Remove from project" });
  fireEvent.pointerDown(removeItem);
  fireEvent.click(removeItem);

  expect(onRemoveFromProject).toHaveBeenCalledWith(threads[0]);
});
