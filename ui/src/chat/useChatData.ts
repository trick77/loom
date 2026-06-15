import { useCallback, useEffect, useMemo, useState } from "react";

import {
  AuthExpiredError,
  getMcpStatus,
  getThread,
  listProjects,
  listThreads,
  type LoadedMessage,
  type McpStatusEvent,
  type Project,
  type Thread,
} from "../api";
import { normalizeActivityTrace } from "../activityTrace";
import type { RouteState } from "./routing";
import { composerAttachmentFromMessageAttachment } from "./useDocumentAttachments";
import type { MessageWithActivityTrace } from "./types";

export function useChatData({
  activeThreadIDRef,
  clearActivityTrace,
  handleActionError,
  onSessionExpired,
  setStreamingArtifacts,
  setStreamingText,
  streamAbortRef,
  streamingThreadIDRef,
}: {
  activeThreadIDRef: React.MutableRefObject<string | null>;
  clearActivityTrace(): void;
  handleActionError(error: unknown, fallback: string, setError: (message: string) => void): void;
  onSessionExpired(): void;
  setStreamingArtifacts(artifacts: []): void;
  setStreamingText(text: string): void;
  streamAbortRef: React.MutableRefObject<AbortController | null>;
  streamingThreadIDRef: React.MutableRefObject<string | null>;
}) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [projectThreads, setProjectThreads] = useState<Thread[]>([]);
  const [chatDataLoaded, setChatDataLoaded] = useState(false);
  const [activeThread, setActiveThread] = useState<Thread | null>(null);
  const [messages, setMessages] = useState<MessageWithActivityTrace[]>([]);
  const [loadError, setLoadError] = useState("");
  const [mcpStatus, setMcpStatus] = useState<McpStatusEvent | null>(null);

  useEffect(() => {
    let active = true;
    Promise.all([listProjects(), listThreads({ limit: 30 })])
      .then(([nextProjects, nextThreads]) => {
        if (!active) return;
        setProjects(nextProjects);
        setThreads(nextThreads.items);
        setChatDataLoaded(true);
        setLoadError("");
      })
      .catch((error: unknown) => {
        if (!active) return;
        setChatDataLoaded(true);
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
          return;
        }
        setLoadError("Chat data failed to load.");
      });
    return () => {
      active = false;
      streamAbortRef.current?.abort();
    };
  }, [onSessionExpired, streamAbortRef]);

  useEffect(() => {
    let active = true;
    getMcpStatus()
      .then((status) => {
        if (!active) return;
        setMcpStatus(status);
      })
      .catch((error: unknown) => {
        if (!active) return;
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
        }
      });
    return () => {
      active = false;
    };
  }, [onSessionExpired]);

  const loadRoute = useCallback((route: RouteState) => {
    if (route.view !== "chat") {
      activeThreadIDRef.current = null;
      setActiveThread(null);
      setMessages([]);
      if (streamingThreadIDRef.current === null) {
        setStreamingText("");
        setStreamingArtifacts([]);
        clearActivityTrace();
      }
      return;
    }
    if (activeThreadIDRef.current === route.threadID) return;
    let active = true;
    getThread(route.threadID)
      .then((response) => {
        if (!active) return;
        setActiveThread(response.thread);
        activeThreadIDRef.current = response.thread.id;
        setMessages(response.messages.map(rehydrateLoadedMessage));
        if (streamingThreadIDRef.current === null) {
          setStreamingText("");
          setStreamingArtifacts([]);
          clearActivityTrace();
        }
      })
      .catch((error: unknown) => {
        if (!active) return;
        handleActionError(error, "Chat failed to load.", setLoadError);
      });
    return () => {
      active = false;
    };
  }, [
    activeThreadIDRef,
    clearActivityTrace,
    handleActionError,
    setStreamingArtifacts,
    setStreamingText,
    streamingThreadIDRef,
  ]);

  const loadProjectThreads = useCallback((route: RouteState) => {
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
        handleActionError(error, "Project chats failed to load.", setLoadError);
      });
    return () => {
      active = false;
    };
  }, [handleActionError]);

  const starredThreads = useMemo(() => threads.filter((thread) => thread.starred), [threads]);
  const recentThreads = useMemo(() => threads.filter((thread) => !thread.starred), [threads]);
  const starredProjects = useMemo(() => projects.filter((project) => project.starred), [projects]);
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
    return projects.find((project) => project.id === activeThread.projectId) ?? null;
  }, [activeThread?.projectId, projects]);

  return {
    activeProject,
    activeThread,
    activeThreadProject,
    chatDataLoaded,
    loadError,
    loadProjectThreads,
    loadRoute,
    mcpStatus,
    messages,
    projectThreads,
    projects,
    recentThreads,
    setActiveThread,
    setMessages,
    setMcpStatus,
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
function rehydrateLoadedMessage(message: LoadedMessage): MessageWithActivityTrace {
  return {
    ...message,
    activityTrace: normalizeActivityTrace(message.activityTrace),
    attachments:
      message.attachments !== undefined && message.attachments.length > 0
        ? message.attachments.map(composerAttachmentFromMessageAttachment)
        : undefined,
  };
}
