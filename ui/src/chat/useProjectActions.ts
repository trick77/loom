import { useCallback, useState } from "react";

import {
  archiveProject,
  createProject,
  deleteProject,
  unarchiveProject,
  updateProject,
  type Project,
  type Thread,
} from "../api";
import i18n from "../i18n";
import type { RouteState } from "./routing";

export function useProjectActions({
  route,
  navigateToProject,
  navigateToProjects,
  setModalError,
  closeThreadMenu,
  setProjects,
  setProjectThreads,
  setThreads,
  handleActionError,
}: {
  route: RouteState;
  navigateToProject(project: Project): void;
  navigateToProjects(): void;
  setModalError(message: string): void;
  closeThreadMenu(): void;
  setProjects(update: (current: Project[]) => Project[]): void;
  setProjectThreads(update: Thread[] | ((current: Thread[]) => Thread[])): void;
  setThreads(update: (current: Thread[]) => Thread[]): void;
  handleActionError(
    error: unknown,
    fallback: string,
    setError: (message: string) => void,
  ): void;
}) {
  // undefined = closed, null = create, Project = edit.
  const [editingProject, setEditingProject] = useState<
    Project | null | undefined
  >(undefined);
  const [deletingProject, setDeletingProject] = useState<Project | null>(null);
  const [archivingProject, setArchivingProject] = useState<Project | null>(
    null,
  );
  const [isMutatingProject, setIsMutatingProject] = useState(false);

  // The openers are stable so the memoized sidebar, which hands them to every
  // project row, does not re-render with the shell.
  const openProjectDialog = useCallback(
    (project: Project | null) => {
      setEditingProject(project);
      setModalError("");
      closeThreadMenu();
    },
    [setModalError, closeThreadMenu],
  );

  const openArchiveProjectModal = useCallback(
    (project: Project) => {
      setArchivingProject(project);
      setModalError("");
      closeThreadMenu();
    },
    [setModalError, closeThreadMenu],
  );

  const openDeleteProjectModal = useCallback(
    (project: Project) => {
      setDeletingProject(project);
      setModalError("");
      closeThreadMenu();
    },
    [setModalError, closeThreadMenu],
  );

  async function handleProjectDialogSubmit(input: {
    name: string;
    description: string;
  }) {
    if (editingProject === undefined || isMutatingProject) return;
    setIsMutatingProject(true);
    try {
      const project =
        editingProject === null
          ? await createProject(input)
          : await updateProject(editingProject.id, input);
      setProjects((current) => [
        project,
        ...current.filter((item) => item.id !== project.id),
      ]);
      setEditingProject(undefined);
      setModalError("");
      if (editingProject === null) {
        navigateToProject(project);
      }
    } catch (error) {
      handleActionError(
        error,
        i18n.t("errors.projectSaveFailed"),
        setModalError,
      );
    } finally {
      setIsMutatingProject(false);
    }
  }

  async function handleArchiveProjectConfirm() {
    if (archivingProject === null || isMutatingProject) return;
    const project = archivingProject;
    setIsMutatingProject(true);
    try {
      await archiveProject(project.id);
      // Mirror the backend's derived visibility on the client: drop the project
      // from the active list and its threads from the sidebar recents so they
      // vanish immediately without a refetch.
      setProjects((current) =>
        current.filter((item) => item.id !== project.id),
      );
      setThreads((current) =>
        current.filter((thread) => thread.projectId !== project.id),
      );
      setArchivingProject(null);
      if (route.view === "project" && route.projectID === project.id) {
        navigateToProjects();
      }
      closeThreadMenu();
      setModalError("");
    } catch (error) {
      handleActionError(
        error,
        i18n.t("errors.projectArchiveFailed"),
        setModalError,
      );
    } finally {
      setIsMutatingProject(false);
    }
  }

  async function handleUnarchiveProject(project: Project) {
    if (isMutatingProject) return;
    setIsMutatingProject(true);
    try {
      await unarchiveProject(project.id);
      // Promote back into the active list with the archived marker cleared so
      // the name badge does not leak into "Your projects".
      setProjects((current) => [
        { ...project, archivedAt: undefined },
        ...current.filter((item) => item.id !== project.id),
      ]);
      closeThreadMenu();
      setModalError("");
    } catch (error) {
      handleActionError(
        error,
        i18n.t("errors.projectUnarchiveFailed"),
        setModalError,
      );
    } finally {
      setIsMutatingProject(false);
    }
  }

  async function handleDeleteProjectConfirm() {
    if (deletingProject === null || isMutatingProject) return;
    const project = deletingProject;
    setIsMutatingProject(true);
    try {
      await deleteProject(project.id);
      setProjects((current) =>
        current.filter((item) => item.id !== project.id),
      );
      setThreads((current) =>
        current.filter((thread) => thread.projectId !== project.id),
      );
      setProjectThreads([]);
      setDeletingProject(null);
      if (route.view === "project" && route.projectID === project.id) {
        navigateToProjects();
      }
      setModalError("");
    } catch (error) {
      handleActionError(
        error,
        i18n.t("errors.projectDeleteFailed"),
        setModalError,
      );
    } finally {
      setIsMutatingProject(false);
    }
  }

  return {
    archivingProject,
    deletingProject,
    editingProject,
    isMutatingProject,
    openArchiveProjectModal,
    openDeleteProjectModal,
    openProjectDialog,
    setArchivingProject,
    setDeletingProject,
    setEditingProject,
    handleArchiveProjectConfirm,
    handleUnarchiveProject,
    handleDeleteProjectConfirm,
    handleProjectDialogSubmit,
  };
}
