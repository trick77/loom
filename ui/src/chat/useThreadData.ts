import { useCallback, useEffect, useMemo, useState } from "react";

import {
  AuthExpiredError,
  getThread,
  listProjects,
  listThreads,
  type LoadedMessage,
  type Project,
  type ShareInfo,
  type Thread,
} from "../api";
import { normalizeActivityTrace } from "../activityTrace";
import i18n from "../i18n";
import type { RouteState } from "./routing";
import { composerAttachmentFromMessageAttachment } from "./useDocumentAttachments";
import type { MessageWithActivityTrace } from "./types";

export function useThreadData({
  abortAllStreamRuns,
  activeThreadIDRef,
  handleActionError,
  onSessionExpired,
}: {
  // Only ever called on unmount: route changes deliberately leave running turns
  // alone, so switching threads no longer touches stream state at all.
  abortAllStreamRuns(): void;
  activeThreadIDRef: React.MutableRefObject<string | null>;
  handleActionError(
    error: unknown,
    fallback: string,
    setError: (message: string) => void,
  ): void;
  onSessionExpired(): void;
}) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [projectThreads, setProjectThreads] = useState<Thread[]>([]);
  const [threadDataLoaded, setThreadDataLoaded] = useState(false);
  const [activeThread, setActiveThread] = useState<Thread | null>(null);
  const [activeShare, setActiveShare] = useState<ShareInfo | null>(null);
  const [messages, setMessages] = useState<MessageWithActivityTrace[]>([]);
  const [loadError, setLoadError] = useState("");

  useEffect(() => {
    let active = true;
    Promise.all([listProjects(), listThreads({ limit: 30 })])
      .then(([nextProjects, nextThreads]) => {
        if (!active) return;
        setProjects(nextProjects);
        setThreads(nextThreads.items);
        setThreadDataLoaded(true);
        setLoadError("");
      })
      .catch((error: unknown) => {
        if (!active) return;
        setThreadDataLoaded(true);
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
          return;
        }
        setLoadError(i18n.t("errors.threadDataLoadFailed"));
      });
    return () => {
      active = false;
      abortAllStreamRuns();
    };
  }, [abortAllStreamRuns, onSessionExpired]);

  const loadRoute = useCallback(
    (route: RouteState) => {
      if (route.view !== "thread") {
        activeThreadIDRef.current = null;
        setActiveThread(null);
        setActiveShare(null);
        setMessages([]);
        return;
      }
      if (activeThreadIDRef.current === route.threadID) return;
      let active = true;
      getThread(route.threadID)
        .then((response) => {
          if (!active) return;
          setActiveThread(response.thread);
          setActiveShare(response.share ?? null);
          activeThreadIDRef.current = response.thread.id;
          setMessages(response.messages.map(rehydrateLoadedMessage));
        })
        .catch((error: unknown) => {
          if (!active) return;
          handleActionError(
            error,
            i18n.t("errors.threadLoadFailed"),
            setLoadError,
          );
        });
      return () => {
        active = false;
      };
    },
    [activeThreadIDRef, handleActionError],
  );

  const loadProjectThreads = useCallback(
    (route: RouteState) => {
      if (route.view !== "project") {
        setProjectThreads([]);
        return;
      }
      let active = true;
      listThreads({ projectId: route.projectID, limit: 1000 })
        .then((nextThreads) => {
          if (!active) return;
          setProjectThreads(nextThreads.items);
          setLoadError("");
        })
        .catch((error: unknown) => {
          if (!active) return;
          handleActionError(
            error,
            i18n.t("errors.projectThreadsLoadFailed"),
            setLoadError,
          );
        });
      return () => {
        active = false;
      };
    },
    [handleActionError],
  );

  const starredThreads = useMemo(
    () => threads.filter((thread) => thread.starred),
    [threads],
  );
  const recentThreads = useMemo(
    () => threads.filter((thread) => !thread.starred),
    [threads],
  );
  const starredProjects = useMemo(
    () => projects.filter((project) => project.starred),
    [projects],
  );
  const unstarredProjects = useMemo(
    () => projects.filter((project) => !project.starred),
    [projects],
  );

  function activeProject(route: RouteState) {
    if (route.view !== "project") return null;
    return projects.find((project) => project.id === route.projectID) ?? null;
  }

  const activeThreadProject = useMemo(() => {
    if (activeThread?.projectId === undefined) return null;
    return (
      projects.find((project) => project.id === activeThread.projectId) ?? null
    );
  }, [activeThread?.projectId, projects]);

  return {
    activeProject,
    activeThread,
    activeShare,
    setActiveShare,
    activeThreadProject,
    threadDataLoaded,
    loadError,
    loadProjectThreads,
    loadRoute,
    messages,
    projectThreads,
    projects,
    recentThreads,
    setActiveThread,
    setMessages,
    setProjectThreads,
    setProjects,
    setThreads,
    starredProjects,
    starredThreads,
    threads,
    unstarredProjects,
  };
}

// rehydrateLoadedMessage turns a message as it arrives from the backend into the
// rendered/stateful shape: it normalizes the activity trace and converts the
// persisted attachments (MessageAttachment[]) into the ComposerAttachment[] the
// sent-message renderer expects, so a reloaded message's previews look identical
// to one that was just sent.
function rehydrateLoadedMessage(
  message: LoadedMessage,
): MessageWithActivityTrace {
  return {
    ...message,
    activityTrace: normalizeActivityTrace(message.activityTrace),
    attachments:
      message.attachments !== undefined && message.attachments.length > 0
        ? message.attachments.map(composerAttachmentFromMessageAttachment)
        : undefined,
  };
}
