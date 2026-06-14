import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AuthExpiredError,
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  createThread,
  listThreads,
  setProjectStarred,
  setThreadStarred,
  stopMessage,
  streamMessage,
  type Artifact,
  type McpStatusEvent,
  type Project,
  type Thread,
  type User,
} from "../api";
import {
  appendReasoningDelta,
  applyReasoningTitle,
  completeTrace,
  upsertTraceToolCall,
  upsertTraceToolResult,
} from "../activityTrace";
import { ChatsPage } from "../ChatsPage";
import { ArtifactsPage } from "../artifacts/ArtifactsPage";
import { MemoryPage } from "../MemoryPage";
import { navigate, routeFromLocation, type RouteState } from "./routing";
import type { MessageWithActivityTrace } from "./types";
import { SettingsModal } from "../settings/SettingsModal";
import { useMediaQuery } from "./useMediaQuery";
import { useActivityTrace } from "./useActivityTrace";
import {
  createComposerAttachment,
  isImageAttachment,
  toSentAttachment,
  useDocumentAttachments,
  type ComposerAttachment,
} from "./useDocumentAttachments";
import { useChatData } from "./useChatData";
import { useProjectActions } from "./useProjectActions";
import { useThreadActions } from "./useThreadActions";
import { ChatPanel } from "./ChatPanel";
import { StartPanel } from "./StartPanel";
import { Sidebar } from "./Sidebar";
import { DeleteThreadModal, RenameThreadModal } from "./threadModals";
import { DeleteProjectModal } from "../projects/DeleteProjectModal";
import { ProjectDetailPage } from "../projects/ProjectDetailPage";
import { ProjectDialog } from "../projects/ProjectDialog";
import { ProjectPickerDialog } from "../projects/ProjectPickerDialog";
import { ProjectsPage } from "../projects/ProjectsPage";
import { replaceThreadById, upsertThreadById } from "../projects/projectMembership";
import { updateMessageAttachment } from "./chatUtils";
import { isWithinUploadSizeLimit } from "./attachmentFiles";

export { buildImageStats } from "./artifacts";
export { GeneratedArtifactCard } from "./GeneratedArtifactCard";
export { ProseMarkdown } from "./messages";

type ChatShellProps = {
  user: User;
  adminPanel: React.ReactNode;
  showAdmin: boolean;
  onAdmin(): void;
  onChat(): void;
  onLogout(): void;
  onSessionExpired(): void;
};

export function ChatShell({
  user,
  adminPanel,
  showAdmin,
  onAdmin,
  onChat,
  onLogout,
  onSessionExpired,
}: ChatShellProps) {
  const [route, setRoute] = useState<RouteState>(() => routeFromLocation());
  const [draft, setDraft] = useState("");
  // Files attached on the new-chat start screen, held until the first send creates
  // a thread to bind them to (deferred upload — avoids orphan empty threads and
  // scopes the upload to the chat it was attached in).
  const [pendingAttachments, setPendingAttachments] = useState<ComposerAttachment[]>([]);
  const [pendingAttachNote, setPendingAttachNote] = useState("");
  const [openThreadMenuID, setOpenThreadMenuID] = useState<string | null>(null);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [modalError, setModalError] = useState("");
  const [streamingText, setStreamingText] = useState("");
  const [streamingArtifacts, setStreamingArtifacts] = useState<Artifact[]>([]);
  const {
    trace: activityTrace,
    traceRef: activityTraceRef,
    toolPending,
    setToolPending,
    update: updateActivityTrace,
    clear: clearActivityTrace,
  } = useActivityTrace();
  // Flush hook for the deferred new-chat upload: the scope is supplied per call at
  // send time (the thread does not exist yet when the file is picked). Its
  // attachNote carries ingestion status/errors after the start screen is gone, so
  // it is surfaced in the chat panel the user lands on.
  const { attachNote: deferredAttachNote, uploadExistingAttachments: flushPendingAttachments } =
    useDocumentAttachments({});
  const [sendError, setSendError] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [streamingThreadID, setStreamingThreadID] = useState<string | null>(null);
  const [isUpdatingStar, setIsUpdatingStar] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const isMobile = useMediaQuery("(max-width: 767px)");
  // On mobile the sidebar is an overlay drawer that always shows the full
  // content; the rail-collapse only applies on desktop.
  const railCollapsed = !isMobile && sidebarCollapsed;
  useEffect(() => {
    if (!mobileSidebarOpen) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setMobileSidebarOpen(false);
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [mobileSidebarOpen]);
  const [threadMutationVersion, setThreadMutationVersion] = useState(0);
  const activeThreadIDRef = useRef<string | null>(null);
  const streamAbortRef = useRef<AbortController | null>(null);
  const streamingThreadIDRef = useRef<string | null>(null);

  const handleActionError = useCallback(
    (error: unknown, fallback: string, setError: (message: string) => void) => {
      if (error instanceof AuthExpiredError) {
        onSessionExpired();
        return;
      }
      setError(error instanceof Error && error.message !== "" ? error.message : fallback);
    },
    [onSessionExpired],
  );

  const setActiveStreamingThreadID = useCallback((threadID: string | null) => {
    streamingThreadIDRef.current = threadID;
    setStreamingThreadID(threadID);
  }, []);

  const {
    activeProject: activeProjectForRoute,
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
  } = useChatData({
    activeThreadIDRef,
    clearActivityTrace,
    handleActionError,
    onSessionExpired,
    setStreamingArtifacts,
    setStreamingText,
    streamAbortRef,
    streamingThreadIDRef,
  });

  const handleStopResponse = useCallback(() => {
    if (!isSending) return;
    const threadID = streamingThreadIDRef.current;
    if (threadID !== null) {
      void stopMessage(threadID).catch((error: unknown) => {
        handleActionError(error, "Message failed to stop.", setSendError);
      });
    }
    streamAbortRef.current?.abort();
  }, [handleActionError, isSending]);

  useEffect(() => {
    if (window.location.pathname === "/") {
      window.history.replaceState({}, "", "/new");
      setRoute({ view: "new" });
    }
    function handlePopState() {
      setRoute(routeFromLocation());
    }
    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  }, []);

  useEffect(() => {
    if (openThreadMenuID === null) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpenThreadMenuID(null);
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [openThreadMenuID]);

  useEffect(() => {
    if (!isSending) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      if (activeThreadIDRef.current !== streamingThreadIDRef.current) return;
      event.preventDefault();
      handleStopResponse();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [handleStopResponse, isSending]);

  useEffect(() => {
    const cleanup = loadRoute(route);
    setSendError("");
    return cleanup;
  }, [loadRoute, route]);

  // Drop files staged on the start screen if the user leaves it without sending,
  // so they can't bind to a different chat later.
  useEffect(() => {
    if (route.view !== "new") {
      setPendingAttachments((current) => {
        current.forEach((attachment) => {
          if (attachment.previewUrl !== undefined) URL.revokeObjectURL(attachment.previewUrl);
        });
        return [];
      });
      setPendingAttachNote("");
    }
  }, [route.view]);

  useEffect(() => {
    return loadProjectThreads(route);
  }, [loadProjectThreads, route]);

  const displayName = user.displayName || user.username;
  const activeProject = activeProjectForRoute(route);

  const navigateToNew = useCallback(() => {
    onChat();
    setMobileSidebarOpen(false);
    activeThreadIDRef.current = null;
    setActiveThread(null);
    setMessages([]);
    if (streamingThreadIDRef.current === null) {
      setStreamingText("");
      setStreamingArtifacts([]);
      clearActivityTrace();
    }
    setSendError("");
    navigate({ view: "new" });
    setRoute({ view: "new" });
  }, [clearActivityTrace, onChat]);

  const navigateToChats = useCallback(() => {
    onChat();
    setMobileSidebarOpen(false);
    navigate({ view: "chats" });
    setRoute({ view: "chats" });
  }, [onChat]);

  const navigateToArtifacts = useCallback(() => {
    onChat();
    setMobileSidebarOpen(false);
    navigate({ view: "artifacts" });
    setRoute({ view: "artifacts" });
  }, [onChat]);

  const navigateToProjects = useCallback(() => {
    onChat();
    setMobileSidebarOpen(false);
    navigate({ view: "projects" });
    setRoute({ view: "projects" });
  }, [onChat]);

  const navigateToMemory = useCallback(() => {
    onChat();
    setMobileSidebarOpen(false);
    navigate({ view: "memory" });
    setRoute({ view: "memory" });
  }, [onChat]);

  const navigateToProject = useCallback(
    (project: Project) => {
      onChat();
      setMobileSidebarOpen(false);
      navigate({ view: "project", projectID: project.id });
      setRoute({ view: "project", projectID: project.id });
    },
    [onChat],
  );

  const {
    deletingProject,
    editingProject,
    isMutatingProject,
    openProjectDialog,
    setDeletingProject,
    setEditingProject,
    handleArchiveProject,
    handleDeleteProjectConfirm,
    handleProjectDialogSubmit,
  } = useProjectActions({
    route,
    navigateToProject,
    navigateToProjects,
    setModalError,
    setOpenThreadMenuID,
    setProjects,
    setProjectThreads,
    setThreads,
    handleActionError,
  });

  const {
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
  } = useThreadActions({
    activeThread,
    activeThreadIDRef,
    setActiveThread,
    setModalError,
    setOpenThreadMenuID,
    setProjectThreads,
    setThreadMutationVersion,
    setThreads,
    handleActionError,
    onActiveThreadArchived: navigateToNew,
    onActiveThreadRemoved: () => {
      streamAbortRef.current?.abort();
      activeThreadIDRef.current = null;
      setActiveThread(null);
      setMessages([]);
      setStreamingText("");
      setStreamingArtifacts([]);
      clearActivityTrace();
      setSendError("");
      navigate({ view: "new" });
      setRoute({ view: "new" });
    },
    onOpenThreadModal: () => setMobileSidebarOpen(false),
    route,
  });

  const reloadThreads = useCallback(() => {
    listThreads({ limit: 30 })
      .then((nextThreads) => setThreads(nextThreads.items))
      .catch((error: unknown) => {
        if (error instanceof AuthExpiredError) onSessionExpired();
      });
  }, [onSessionExpired]);

  async function selectThread(threadID: string) {
    onChat();
    setMobileSidebarOpen(false);
    navigate({ view: "chat", threadID });
    setRoute({ view: "chat", threadID });
  }

  async function handleSetThreadStarred(thread: Thread, starred: boolean, menuKey?: string) {
    if (isUpdatingStar) return;
    setIsUpdatingStar(true);
    try {
      const updatedThread = await setThreadStarred(thread.id, starred);
      if (activeThreadIDRef.current === updatedThread.id) {
        setActiveThread(updatedThread);
      }
      setThreads((current) =>
        current.map((item) => (item.id === updatedThread.id ? updatedThread : item)),
      );
      setProjectThreads((current) => replaceThreadById(current, updatedThread));
      setThreadMutationVersion((value) => value + 1);
      if (menuKey !== undefined) {
        setOpenThreadMenuID(null);
      }
      setSendError("");
    } catch (error) {
      handleActionError(error, "Thread failed to update.", setSendError);
    } finally {
      setIsUpdatingStar(false);
    }
  }

  async function handleSetProjectStarred(project: Project, starred: boolean, menuKey?: string) {
    if (isUpdatingStar) return;
    setIsUpdatingStar(true);
    try {
      const updatedProject = await setProjectStarred(project.id, starred);
      setProjects((current) =>
        current.map((item) => (item.id === updatedProject.id ? updatedProject : item)),
      );
      if (menuKey !== undefined) {
        setOpenThreadMenuID(null);
      }
      setSendError("");
    } catch (error) {
      handleActionError(error, "Project failed to update.", setSendError);
    } finally {
      setIsUpdatingStar(false);
    }
  }

  function handleAttachPendingFiles(files: File[]) {
    setSendError("");
    const sizeFiltered = files.filter(isWithinUploadSizeLimit);
    if (sizeFiltered.length < files.length) {
      setPendingAttachNote("Files must be 25 MB or smaller.");
    }
    setPendingAttachments((current) => {
      const remaining = DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE - current.length;
      if (remaining <= 0) {
        setPendingAttachNote(`You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`);
        return current;
      }
      const accepted = sizeFiltered.slice(0, remaining);
      if (accepted.length < sizeFiltered.length) {
        setPendingAttachNote(`You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`);
      } else if (accepted.length > 0 && sizeFiltered.length === files.length) {
        setPendingAttachNote("");
      }
      return [
        ...current,
        ...accepted.map((file) => createComposerAttachment(file, "queued")),
      ];
    });
  }

  function handleRemovePendingAttachment(id: string) {
    setSendError("");
    setPendingAttachments((current) => {
      const removed = current.find((attachment) => attachment.id === id);
      if (removed?.previewUrl !== undefined) URL.revokeObjectURL(removed.previewUrl);
      return current.filter((attachment) => attachment.id !== id);
    });
    setPendingAttachNote("");
  }

  async function handleSend(attachments: ComposerAttachment[] = pendingAttachments.map(toSentAttachment)) {
    const content = draft.trim();
    if (content === "" || isSending) return;
    await sendContent(content, { restoreDraftOnError: true, attachments });
  }

  async function handleRetry(content: string) {
    if (content.trim() === "" || isSending || activeThread === null) return;
    await sendContent(content, { restoreDraftOnError: false, attachments: [] });
  }

  async function sendContent(
    content: string,
    options: { restoreDraftOnError: boolean; attachments: ComposerAttachment[] },
  ) {
    setDraft("");
    setIsSending(true);
    setStreamingText("");
    setStreamingArtifacts([]);
    clearActivityTrace();
    setSendError("");
    let abortController: AbortController | null = null;
    let createdThreadForFallback: Thread | null = null;
    let receivedThreadEvent = false;
    let keepFailedTurnVisible = false;
    const projectIDForNewThread = route.view === "project" ? route.projectID : null;
    const updateSentAttachmentStatus = (id: string, patch: Partial<ComposerAttachment>) => {
      const attachment = options.attachments.find((item) => item.id === id);
      if (attachment !== undefined) Object.assign(attachment, patch);
      setMessages((current) => updateMessageAttachment(current, id, patch));
    };
    try {
      let targetThread = activeThread;
      if (targetThread === null) {
        targetThread =
          projectIDForNewThread === null
            ? await createThread({ title: content })
            : await createThread({ projectId: projectIDForNewThread, title: content });
        createdThreadForFallback = targetThread;
        // Now that a thread exists, flush files attached on the start screen,
        // bound to it (project-less => private to this chat). Image uploads must
        // finish before the first model request so their artifact ids can be sent
        // as multimodal inputs; document indexing still continues in the background.
        if (pendingAttachments.length > 0) {
          await flushPendingAttachments(
            pendingAttachments,
            {
              threadId: targetThread.id,
              projectId: projectIDForNewThread ?? undefined,
            },
            updateSentAttachmentStatus,
          );
          const failedImageAttachment = options.attachments.find(
            (attachment) =>
              isImageAttachment(attachment) &&
              (attachment.status === "error" || attachment.artifactId === undefined),
          );
          if (failedImageAttachment !== undefined) {
            throw new Error(failedImageAttachment.error ?? `Failed to upload ${failedImageAttachment.filename}.`);
          }
          // Keep the object URL alive for the optimistic sent bubble.
          setPendingAttachments([]);
        }
        setActiveThread(targetThread);
        activeThreadIDRef.current = targetThread.id;
        setMessages([]);
        navigate({ view: "chat", threadID: targetThread.id });
        setRoute({ view: "chat", threadID: targetThread.id });
      }
      const targetThreadID = targetThread.id;
      activeThreadIDRef.current = targetThreadID;
      abortController = new AbortController();
      streamAbortRef.current?.abort();
      streamAbortRef.current = abortController;
      setActiveStreamingThreadID(targetThreadID);
      const isCurrentThread = () => activeThreadIDRef.current === targetThreadID;
      const imageAttachmentIds = options.attachments
        .filter((attachment) => isImageAttachment(attachment) && attachment.artifactId !== undefined)
        .map((attachment) => attachment.artifactId!);
      const documentAttachmentIds = options.attachments
        .filter((attachment) => !isImageAttachment(attachment) && attachment.documentId !== undefined)
        .map((attachment) => attachment.documentId!);
      await streamMessage(targetThreadID, content, {
        onUserMessage: (message) => {
          if (isCurrentThread()) {
            setMessages((current) => [
              ...current,
              options.attachments.length > 0
                ? { ...message, attachments: options.attachments.map(toSentAttachment) }
                : message,
            ]);
          }
        },
        onDelta: (delta) => {
          setStreamingText((current) => current + delta);
        },
        onReasoningDelta: (delta) => {
          updateActivityTrace((current) => appendReasoningDelta(current, delta));
        },
        onReasoningTitle: (event) => {
          updateActivityTrace((current) => applyReasoningTitle(current, event.id, event.title));
        },
        onToolPending: () => {
          setToolPending(true);
        },
        onToolCall: (event) => {
          // The pending call is now a real (running) trace event; let the trace's
          // own running status drive the "thinking" affordance from here.
          setToolPending(false);
          updateActivityTrace((current) => upsertTraceToolCall(current, event));
        },
        onToolResult: (event) => {
          updateActivityTrace((current) => upsertTraceToolResult(current, event));
        },
        onArtifact: (artifact) => {
          setStreamingArtifacts((current) => [
            ...current.filter((item) => item.id !== artifact.id),
            artifact,
          ]);
        },
        onAssistantMessage: (message) => {
          const completedTrace = completeTrace(activityTraceRef.current);
          if (isCurrentThread()) {
            setMessages((current) => [
              ...current,
              completedTrace.length > 0
                ? {
                    ...message,
                    activityTrace: completedTrace,
                  }
                : message,
            ]);
          }
          setStreamingText("");
          setStreamingArtifacts([]);
          clearActivityTrace();
        },
        onThread: (updatedThread) => {
          receivedThreadEvent = true;
          if (isCurrentThread()) setActiveThread(updatedThread);
          setThreads((current) => upsertThread(current, updatedThread));
          if (
            route.view === "project" &&
            updatedThread.projectId !== undefined &&
            updatedThread.projectId === route.projectID
          ) {
            setProjectThreads((current) => upsertThreadById(current, updatedThread));
          }
        },
        onProject: (updatedProject) => {
          setProjects((current) => upsertProject(current, updatedProject));
        },
        onMcpStatus: (event) => setMcpStatus(event),
      }, abortController.signal, { imageAttachmentIds, documentAttachmentIds });
      const fallbackThread = createdThreadForFallback;
      if (!receivedThreadEvent && fallbackThread !== null) {
        setThreads((current) => upsertThread(current, fallbackThread));
        if (
          route.view === "project" &&
          fallbackThread.projectId !== undefined &&
          fallbackThread.projectId === route.projectID
        ) {
          setProjectThreads((current) => upsertThreadById(current, fallbackThread));
        }
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      setStreamingText("");
      setStreamingArtifacts([]);
      // Keep any activity trace visible so a failed turn still shows what was
      // attempted (e.g. a tool that errored); the next send clears it.
      keepFailedTurnVisible = true;
      if (options.restoreDraftOnError) setDraft(content);
      handleActionError(error, "Message failed to send.", setSendError);
    } finally {
      setIsSending(false);
      if (!keepFailedTurnVisible) setActiveStreamingThreadID(null);
      if (abortController !== null && streamAbortRef.current === abortController) {
        streamAbortRef.current = null;
      }
    }
  }

  const activeThreadOwnsStreamState = activeThread !== null && streamingThreadID === activeThread.id;
  const activeThreadIsStreaming = isSending && activeThreadOwnsStreamState;
  const visibleStreamingText = activeThreadOwnsStreamState ? streamingText : "";
  const visibleStreamingArtifacts = activeThreadOwnsStreamState ? streamingArtifacts : [];
  const visibleActivityTrace = activeThreadOwnsStreamState ? activityTrace : [];
  const visibleToolPending = activeThreadOwnsStreamState ? toolPending : false;
  // Keep errors with the thread that owns the active or failed stream state.
  const visibleSendError = streamingThreadID === null || activeThreadOwnsStreamState ? sendError : "";

  return (
    <div
      className={`grid h-svh bg-bg font-sans text-ink transition-[grid-template-columns] duration-200 ease-out grid-cols-[1fr] ${
        sidebarCollapsed ? "md:grid-cols-[56px_1fr]" : "md:grid-cols-[362px_1fr]"
      }`}
    >
      <Sidebar
        user={user}
        displayName={displayName}
        route={route}
        showAdmin={showAdmin}
        isMobile={isMobile}
        sidebarCollapsed={sidebarCollapsed}
        railCollapsed={railCollapsed}
        mobileSidebarOpen={mobileSidebarOpen}
        userMenuOpen={userMenuOpen}
        loadError={loadError}
        projectsAvailable={projects.length > 0}
        starredThreads={starredThreads}
        recentThreads={recentThreads}
        starredProjects={starredProjects}
        unstarredProjects={unstarredProjects}
        openThreadMenuID={openThreadMenuID}
        onToggleDesktopCollapsed={() => setSidebarCollapsed((value) => !value)}
        onCloseMobileSidebar={() => setMobileSidebarOpen(false)}
        onOpenMobileSidebar={() => setMobileSidebarOpen(true)}
        onToggleUserMenu={() => setUserMenuOpen((open) => !open)}
        onCloseUserMenu={() => setUserMenuOpen(false)}
        onOpenSettings={() => setSettingsOpen(true)}
        onLogout={onLogout}
        onAdmin={onAdmin}
        onNewChat={navigateToNew}
        onChats={navigateToChats}
        onArtifacts={navigateToArtifacts}
        onProjects={navigateToProjects}
        onMemory={navigateToMemory}
        onSelectThread={selectThread}
        onDeleteThread={openDeleteModal}
        onRenameThread={openRenameModal}
        onAddThreadToProject={(thread) => {
          setMovingThreads([thread]);
          setModalError("");
        }}
        onStarThread={handleSetThreadStarred}
        onNavigateProject={navigateToProject}
        onStarProject={handleSetProjectStarred}
        onToggleThreadMenu={(menuKey) =>
          setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
        }
        onCloseThreadMenu={() => setOpenThreadMenuID(null)}
      />
      <main className="min-w-0 bg-bg">
        {showAdmin ? (
          adminPanel
        ) : route.view === "chats" ? (
          <ChatsPage
            mutationVersion={threadMutationVersion}
            projectsAvailable={projects.length > 0}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            onNewChat={navigateToNew}
            onSelectThread={(threadID) => void selectThread(threadID)}
            onRenameThread={openRenameModal}
            onDeleteThread={openDeleteModal}
            onStarThread={(thread, starred, menuKey) => void handleSetThreadStarred(thread, starred, menuKey)}
            onAddThreadToProject={(thread) => {
              setMovingThreads([thread]);
              setModalError("");
            }}
            onMoveSelectedToProject={(selectedThreads) => {
              setMovingThreads(selectedThreads);
              setModalError("");
            }}
            onAfterBulkDelete={reloadThreads}
            onSessionExpired={onSessionExpired}
          />
        ) : route.view === "artifacts" ? (
          <ArtifactsPage
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            onSessionExpired={onSessionExpired}
          />
        ) : route.view === "memory" ? (
          <MemoryPage onOpenSidebar={() => setMobileSidebarOpen(true)} />
        ) : route.view === "projects" ? (
          <ProjectsPage
            projects={projects}
            loadError={loadError}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            onCreateProject={() => openProjectDialog(null)}
            onOpenProject={navigateToProject}
            onEditProject={openProjectDialog}
            onArchiveProject={(project) => void handleArchiveProject(project)}
            onDeleteProject={(project) => {
              setDeletingProject(project);
              setModalError("");
              setOpenThreadMenuID(null);
            }}
          />
        ) : route.view === "project" ? (
          activeProject === null ? (
            <ProjectsPage
              projects={projects}
              loadError={loadError === "" && chatDataLoaded ? "Project not found." : loadError}
              onOpenSidebar={() => setMobileSidebarOpen(true)}
              onCreateProject={() => openProjectDialog(null)}
              onOpenProject={navigateToProject}
              onEditProject={openProjectDialog}
              onArchiveProject={(project) => void handleArchiveProject(project)}
              onDeleteProject={(project) => {
                setDeletingProject(project);
                setModalError("");
                setOpenThreadMenuID(null);
              }}
            />
          ) : (
            <ProjectDetailPage
              project={activeProject}
              threads={projectThreads}
              draft={draft}
              sendError={sendError}
              isSending={false}
              sendDisabled={isSending}
              openThreadMenuID={openThreadMenuID}
              onBack={navigateToProjects}
              onDraftChange={setDraft}
              onSend={handleSend}
              onStop={handleStopResponse}
              onOpenThread={(threadID) => void selectThread(threadID)}
              onRenameThread={openRenameModal}
              onDeleteThread={openDeleteModal}
              onArchiveThread={(thread) => void handleArchiveThread(thread)}
              onStarThread={(thread, starred, menuKey) => void handleSetThreadStarred(thread, starred, menuKey)}
              onRemoveFromProject={(thread) => void handleRemoveThreadFromProject(thread)}
              onToggleThreadMenu={(menuKey) =>
                setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
              }
              onCloseThreadMenu={() => setOpenThreadMenuID(null)}
              onEditProject={openProjectDialog}
              onArchiveProject={(project) => void handleArchiveProject(project)}
              onDeleteProject={(project) => {
                setDeletingProject(project);
                setModalError("");
                setOpenThreadMenuID(null);
              }}
              onToggleStar={(project, starred) => void handleSetProjectStarred(project, starred)}
              onOpenSidebar={() => setMobileSidebarOpen(true)}
            />
          )
        ) : route.view === "new" ? (
          <StartPanel
            displayName={displayName}
            draft={draft}
            isSending={false}
            sendDisabled={isSending}
            mcpStatus={mcpStatus}
            sendError={sendError}
            attachments={pendingAttachments}
            attachNote={pendingAttachNote}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            onDraftChange={setDraft}
            onSend={handleSend}
            onStop={handleStopResponse}
            onAttachFiles={handleAttachPendingFiles}
            onAttachError={setPendingAttachNote}
            onRemoveAttachment={handleRemovePendingAttachment}
          />
        ) : (
          <ChatPanel
            thread={activeThread}
            threadProject={activeThreadProject}
            deferredAttachNote={deferredAttachNote}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            messages={messages}
            draft={draft}
            streamingText={visibleStreamingText}
            streamingArtifacts={visibleStreamingArtifacts}
            activityTrace={visibleActivityTrace}
            toolPending={visibleToolPending}
            sendError={visibleSendError}
            isSending={activeThreadIsStreaming}
            sendDisabled={isSending && !activeThreadIsStreaming}
            mcpStatus={mcpStatus}
            openThreadMenuID={openThreadMenuID}
            onDraftChange={setDraft}
            onSend={handleSend}
            onStop={handleStopResponse}
            onRetry={handleRetry}
            onOpenProject={navigateToProject}
            onDeleteThread={openDeleteModal}
            onRenameThread={openRenameModal}
            onAddToProject={
              projects.length === 0
                ? undefined
                : (thread) => {
                    setMovingThreads([thread]);
                    setModalError("");
                  }
            }
            onStarThread={(thread, starred, menuKey) => void handleSetThreadStarred(thread, starred, menuKey)}
            onToggleThreadMenu={(menuKey) =>
              setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
            }
            onCloseThreadMenu={() => setOpenThreadMenuID(null)}
          />
        )}
      </main>
      {renamingThread !== null && (
        <RenameThreadModal
          title={renameTitle}
          error={modalError}
          disabled={isMutatingThread}
          onTitleChange={setRenameTitle}
          onCancel={() => setRenamingThread(null)}
          onSubmit={handleRenameSubmit}
        />
      )}
      {deletingThread !== null && (
        <DeleteThreadModal
          error={modalError}
          disabled={isMutatingThread}
          onCancel={() => setDeletingThread(null)}
          onDelete={handleDeleteConfirm}
        />
      )}
      {editingProject !== undefined && (
        <ProjectDialog
          project={editingProject}
          error={modalError}
          disabled={isMutatingProject}
          onCancel={() => setEditingProject(undefined)}
          onSubmit={(input) => void handleProjectDialogSubmit(input)}
        />
      )}
      {deletingProject !== null && (
        <DeleteProjectModal
          project={deletingProject}
          error={modalError}
          disabled={isMutatingProject}
          onCancel={() => setDeletingProject(null)}
          onDelete={() => void handleDeleteProjectConfirm()}
        />
      )}
      {movingThreads.length > 0 && (
        <ProjectPickerDialog
          threads={movingThreads}
          projects={projects}
          error={modalError}
          disabled={isMutatingThread}
          onCancel={() => setMovingThreads([])}
          onSelect={(project) => void handleMoveThreadsToProject(movingThreads, project)}
        />
      )}
      {settingsOpen && <SettingsModal onClose={() => setSettingsOpen(false)} />}
    </div>
  );
}

function upsertThread(current: Thread[], thread: Thread): Thread[] {
  return [thread, ...current.filter((item) => item.id !== thread.id)];
}

function upsertProject(current: Project[], project: Project): Project[] {
  if (current.some((item) => item.id === project.id)) {
    return current.map((item) => (item.id === project.id ? project : item));
  }
  return [project, ...current];
}
