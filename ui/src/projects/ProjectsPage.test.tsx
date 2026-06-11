import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { Project, Thread } from "../api";
import { ICONS } from "../chat/Icon";
import { ProjectDetailPage } from "./ProjectDetailPage";
import { ProjectsPage } from "./ProjectsPage";

const projects: Project[] = [
  {
    id: "p1",
    name: "Research",
    description: "Paper notes",
    createdAt: "2026-06-10T00:00:00Z",
    updatedAt: "2026-06-10T12:00:00Z",
  },
];

const threads: Thread[] = [
  {
    id: "t1",
    projectId: "p1",
    title: "Literature review",
    starred: false,
    createdAt: "2026-06-10T00:00:00Z",
    updatedAt: "2026-06-10T12:00:00Z",
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
      onDeleteProject={vi.fn()}
    />,
  );

  expect(screen.getByRole("heading", { name: "Projects" })).toBeInTheDocument();
  expect(screen.getByPlaceholderText("Search projects...")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "New project" })).toBeInTheDocument();
  expect(screen.getByText("Research")).toBeInTheDocument();
  expect(screen.getByText("Paper notes")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Sort by Recent activity" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Open project actions for Research" })).toHaveTextContent(
    ICONS.moreVertical,
  );
  expect(screen.queryByText("Example project")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Share" })).not.toBeInTheDocument();
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
      onArchiveThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
      onOpenSidebar={vi.fn()}
    />,
  );

  expect(screen.getByRole("button", { name: "All projects" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Research" })).toBeInTheDocument();
  expect(screen.getByText("Paper notes")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Literature review" })).toBeInTheDocument();
  expect(screen.queryByText("Example project")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Share" })).not.toBeInTheDocument();
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
      onArchiveThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
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
      onDeleteProject={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Open project actions for Research" }));
  const menu = screen.getByRole("menu", { name: "Project actions" });
  expect(within(menu).getByRole("menuitem", { name: "Edit details" })).toBeInTheDocument();
  expect(within(menu).getByRole("menuitem", { name: "Archive" })).toBeInTheDocument();
  expect(within(menu).getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
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
      onArchiveThread={vi.fn()}
      onStarThread={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onEditProject={vi.fn()}
      onArchiveProject={vi.fn()}
      onDeleteProject={vi.fn()}
      onOpenSidebar={vi.fn()}
    />,
  );

  expect(screen.getByRole("button", { name: "Open project actions" })).toHaveTextContent(ICONS.moreVertical);
  expect(screen.getByRole("button", { name: "Open chat actions" })).toHaveTextContent(ICONS.moreVertical);
});
