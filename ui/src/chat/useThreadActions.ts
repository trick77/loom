import { useState } from "react";

import {
  archiveThread,
  deleteThread,
  updateThread,
  type Project,
  type Thread,
} from "../api";
import { removeThreadsById, replaceThreadById, upsertThreadById } from "../projects/projectMembership";
import type { RouteState } from "./routing";

export function useThreadActions({
  activeThread,
  activeThreadIDRef,
  setActiveThread,
  setModalError,
  setOpenThreadMenuID,
  setProjectThreads,
  setThreadMutationVersion,
  setThreads,
  handleActionError,
  onActiveThreadArchived,
  onActiveThreadRemoved,
  onOpenThreadModal,
  route,
}: {
  activeThread: Thread | null;
  activeThreadIDRef: React.MutableRefObject<string | null>;
  setActiveThread(thread: Thread | null): void;
  setModalError(message: string): void;
  setOpenThreadMenuID(menuID: string | null): void;
  setProjectThreads(update: (current: Thread[]) => Thread[]): void;
  setThreadMutationVersion(update: (value: number) => number): void;
  setThreads(update: (current: Thread[]) => Thread[]): void;
  handleActionError(error: unknown, fallback: string, setError: (message: string) => void): void;
  onActiveThreadArchived(): void;
  onActiveThreadRemoved(): void;
  onOpenThreadModal(): void;
  route: RouteState;
}) {
  const [movingThreads, setMovingThreads] = useState<Thread[]>([]);
  const [renamingThread, setRenamingThread] = useState<Thread | null>(null);
  const [deletingThread, setDeletingThread] = useState<Thread | null>(null);
  const [renameTitle, setRenameTitle] = useState("");
  const [isMutatingThread, setIsMutatingThread] = useState(false);

  function openRenameModal(thread: Thread) {
    onOpenThreadModal();
    setRenamingThread(thread);
    setRenameTitle(thread.title);
    setModalError("");
    setOpenThreadMenuID(null);
  }

  function openDeleteModal(thread: Thread) {
    onOpenThreadModal();
    setDeletingThread(thread);
    setModalError("");
    setOpenThreadMenuID(null);
  }

  async function handleRenameSubmit() {
    if (renamingThread === null || isMutatingThread) return;
    const title = renameTitle.trim();
    if (title === "") return;
    setIsMutatingThread(true);
    try {
      const updatedThread = await updateThread(renamingThread.id, { title });
      setThreads((current) => current.map((item) => (item.id === updatedThread.id ? updatedThread : item)));
      setProjectThreads((current) => replaceThreadById(current, updatedThread));
      if (activeThreadIDRef.current === updatedThread.id) {
        setActiveThread(updatedThread);
      }
      setThreadMutationVersion((value) => value + 1);
      setRenamingThread(null);
      setModalError("");
    } catch (error) {
      handleActionError(error, "Thread failed to rename.", setModalError);
    } finally {
      setIsMutatingThread(false);
    }
  }

  async function handleDeleteConfirm() {
    if (deletingThread === null || isMutatingThread) return;
    const thread = deletingThread;
    setIsMutatingThread(true);
    try {
      await deleteThread(thread.id);
      setThreads((current) => current.filter((item) => item.id !== thread.id));
      setProjectThreads((current) => current.filter((item) => item.id !== thread.id));
      setThreadMutationVersion((value) => value + 1);
      if (activeThreadIDRef.current === thread.id) {
        onActiveThreadArchived();
      }
      setDeletingThread(null);
      setModalError("");
    } catch (error) {
      handleActionError(error, "Thread failed to delete.", setModalError);
    } finally {
      setIsMutatingThread(false);
    }
  }

  async function handleArchiveThread(thread: Thread) {
    if (isMutatingThread) return;
    setIsMutatingThread(true);
    try {
      await archiveThread(thread.id);
      setThreads((current) => current.filter((item) => item.id !== thread.id));
      setProjectThreads((current) => current.filter((item) => item.id !== thread.id));
      setThreadMutationVersion((value) => value + 1);
      if (activeThreadIDRef.current === thread.id) {
        onActiveThreadRemoved();
      }
      setOpenThreadMenuID(null);
      setModalError("");
    } catch (error) {
      handleActionError(error, "Thread failed to archive.", setModalError);
    } finally {
      setIsMutatingThread(false);
    }
  }

  async function handleMoveThreadsToProject(targetThreads: Thread[], project: Project) {
    if (isMutatingThread || targetThreads.length === 0) return;
    setIsMutatingThread(true);
    try {
      const results = await Promise.allSettled(
        targetThreads.map(async (thread) => ({
          original: thread,
          updated: await updateThread(thread.id, { projectId: project.id }),
        })),
      );
      const updatedThreads = results
        .filter((result): result is PromiseFulfilledResult<{ original: Thread; updated: Thread }> => result.status === "fulfilled")
        .map((result) => result.value.updated);
      if (updatedThreads.length === 0) {
        throw new Error("No chats moved.");
      }
      const failedThreads = results
        .map((result, index) => (result.status === "rejected" ? targetThreads[index] : null))
        .filter((thread): thread is Thread => thread !== null);
      const updatedIDs = new Set(updatedThreads.map((thread) => thread.id));
      setThreads((current) =>
        current.map((thread) => updatedThreads.find((updated) => updated.id === thread.id) ?? thread),
      );
      setProjectThreads((current) => {
        let next = current;
        if (route.view === "project" && route.projectID !== project.id) {
          next = removeThreadsById(next, updatedIDs);
        }
        if (route.view === "project" && route.projectID === project.id) {
          for (const thread of updatedThreads) {
            next = upsertThreadById(next, thread);
          }
        }
        return next;
      });
      if (activeThread !== null) {
        const updatedActive = updatedThreads.find((thread) => thread.id === activeThread.id);
        if (updatedActive !== undefined) setActiveThread(updatedActive);
      }
      setMovingThreads(failedThreads);
      setThreadMutationVersion((value) => value + 1);
      const failedCount = failedThreads.length;
      setModalError(failedCount > 0 ? `${failedCount} chat${failedCount === 1 ? "" : "s"} failed to move.` : "");
    } catch (error) {
      handleActionError(error, "Chats failed to move.", setModalError);
    } finally {
      setIsMutatingThread(false);
    }
  }

  async function handleRemoveThreadFromProject(thread: Thread) {
    if (isMutatingThread) return;
    setIsMutatingThread(true);
    try {
      const updatedThread = await updateThread(thread.id, { projectId: null });
      setThreads((current) => replaceThreadById(current, updatedThread));
      setProjectThreads((current) => current.filter((item) => item.id !== updatedThread.id));
      if (activeThreadIDRef.current === updatedThread.id) {
        setActiveThread(updatedThread);
      }
      setThreadMutationVersion((value) => value + 1);
      setOpenThreadMenuID(null);
      setModalError("");
    } catch (error) {
      handleActionError(error, "Chat failed to remove from project.", setModalError);
    } finally {
      setIsMutatingThread(false);
    }
  }

  return {
    deletingThread,
    isMutatingThread,
    movingThreads,
    renameTitle,
    renamingThread,
    handleArchiveThread,
    handleDeleteConfirm,
    handleMoveThreadsToProject,
    handleRemoveThreadFromProject,
    handleRenameSubmit,
    openDeleteModal,
    openRenameModal,
    setDeletingThread,
    setMovingThreads,
    setRenameTitle,
    setRenamingThread,
  };
}
