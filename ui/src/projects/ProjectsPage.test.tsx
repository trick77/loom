import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { Project, Thread } from "../api";
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
  expect(screen.queryByText("Example project")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Share" })).not.toBeInTheDocument();
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
