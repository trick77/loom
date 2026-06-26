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
  type ContentBlock,
  type McpStatusEvent,
  type Project,
  type Thread,
  type User,
} from "../api";
import {
  appendArtifactBlock,
  appendReasoningDeltaBlock,
  appendTextDelta,
  applyReasoningTitleBlock,
  graftStreamedBlocks,
  upsertToolCallBlock,
  upsertToolResultBlock,
} from "./contentBlocks";
import { ThreadsPage } from "../ThreadsPage";
import { ArtifactsPage } from "../artifacts/ArtifactsPage";
import { MemoryPage } from "../MemoryPage";
import { navigate, routeFromLocation, type RouteState } from "./routing";
import type { MessageWithActivityTrace } from "./types";
import { SettingsModal } from "../settings/SettingsModal";
import { useMediaQuery } from "./useMediaQuery";
import {
  composerAttachmentFromArtifact,
  createComposerAttachment,
  isImageAttachment,
  toSentAttachment,
  useDocumentAttachments,
  type ComposerAttachment,
} from "./useDocumentAttachments";
import { useThreadData } from "./useThreadData";
import { useProjectActions } from "./useProjectActions";
import { useThreadActions } from "./useThreadActions";
import { ThreadPanel } from "./ThreadPanel";
import { StartPanel } from "./StartPanel";
import { Sidebar } from "./Sidebar";
import { tabTitle } from "./tabTitle";
import { DeleteThreadModal, RenameThreadModal } from "./threadModals";
import { ArchiveProjectModal } from "../projects/ArchiveProjectModal";
import { DeleteProjectModal } from "../projects/DeleteProjectModal";
import { ProjectDetailPage } from "../projects/ProjectDetailPage";
import { ProjectDialog } from "../projects/ProjectDialog";
import { ProjectPickerDialog } from "../projects/ProjectPickerDialog";
import { ProjectsPage } from "../projects/ProjectsPage";
import { replaceThreadById, upsertThreadById } from "../projects/projectMembership";
import { reconcileUserMessage, updateMessageAttachment } from "./threadUtils";
import { isWithinUploadSizeLimit } from "./attachmentFiles";

export { buildImageStats } from "./artifacts";
export { GeneratedArtifactCard } from "./GeneratedArtifactCard";
export { ProseMarkdown } from "./messages";

type ThreadShellProps = {
  user: User;
  adminPanel: React.ReactNode;
  showAdmin: boolean;
  onAdmin(): void;
  onThread(): void;
  onLogout(): void;
  onSessionExpired(): void;
};

export function ThreadShell({
  user,
  adminPanel,
  showAdmin,
  onAdmin,
  onThread,
  onLogout,
  onSessionExpired,
}: ThreadShellProps) {
  const [route, setRoute] = useState<RouteState>(() => routeFromLocation());
  const [draft, setDraft] = useState("");
  // Files attached on the new-thread start screen, held until the first send creates
  // a thread to bind them to (deferred upload — avoids orphan empty threads and
  // scopes the upload to the thread it was attached in).
  const [pendingAttachments, setPendingAttachments] = useState<ComposerAttachment[]>([]);
  const [pendingAttachNote, setPendingAttachNote] = useState("");
  const [openThreadMenuID, setOpenThreadMenuID] = useState<string | null>(null);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [modalError, setModalError] = useState("");
  // Each assistant turn is reconstructed live as a single ordered ContentBlock[]
  // (text / trace / artifact) mirroring the order the SSE events arrive, so the
  // transcript renders text, tool activity and images in true chronological
  // order. The ref mirrors the state so the streaming closures can read the
  // current blocks synchronously (e.g. to graft the completed turn onto the
  // committed message). `toolPending` bridges a model-yielded tool call until its
  // running trace event surfaces, driving the live "thinking" affordance.
  const [streamingBlocks, setStreamingBlocks] = useState<ContentBlock[]>([]);
  const streamingBlocksRef = useRef<ContentBlock[]>([]);
  const [toolPending, setToolPending] = useState(false);
  const clearStreamingBlocks = useCallback(() => {
    streamingBlocksRef.current = [];
    setStreamingBlocks([]);
    setToolPending(false);
  }, []);
  // Flush hook for the deferred new-thread upload: the scope is supplied per call at
  // send time (the thread does not exist yet when the file is picked). Its
  // attachNote carries ingestion status/errors after the start screen is gone, so
  // it is surfaced in the thread panel the user lands on.
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
    threadDataLoaded,
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
  } = useThreadData({
    activeThreadIDRef,
    clearStreamingBlocks,
    handleActionError,
    onSessionExpired,
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
  // so they can't bind to a different thread later.
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
  // Archived projects are absent from the active `projects` list, so fall back
  // to the project object we navigated into so its detail page (threads +
  // Unarchive) still resolves.
  const [openedProject, setOpenedProject] = useState<Project | null>(null);
  const activeProject =
    activeProjectForRoute(route) ??
    (route.view === "project" && openedProject?.id === route.projectID ? openedProject : null);

  useEffect(() => {
    document.title = tabTitle(route, activeThread, activeProject);
  }, [route, activeThread?.title, activeProject?.name]);

  const navigateToNew = useCallback(() => {
    onThread();
    setMobileSidebarOpen(false);
    activeThreadIDRef.current = null;
    setActiveThread(null);
    setMessages([]);
    if (streamingThreadIDRef.current === null) {
      clearStreamingBlocks();
    }
    setSendError("");
    navigate({ view: "new" });
    setRoute({ view: "new" });
  }, [clearStreamingBlocks, onThread]);

  // "Use in thread" from the Artifacts library: open the new-chat screen with the
  // artifact pre-attached so the user can prompt against it. navigateToNew() nulls
  // activeThread (so sendContent creates a fresh thread, not appends to a stale
  // one); setting pendingAttachments in the same synchronous handler is batched
  // with the route switch, so the start-screen clear effect (which only wipes when
  // route.view !== "new") leaves it intact. composerAttachmentFromArtifact carries
  // only the artifact id (no File), so it is referenced on send — never re-uploaded
  // or duplicated — and removing the chip won't delete the original artifact.
  const handleUseArtifactInThread = useCallback(
    (artifact: Artifact) => {
      navigateToNew();
      setPendingAttachments([composerAttachmentFromArtifact(artifact)]);
    },
    [navigateToNew],
  );

  const navigateToThreads = useCallback(() => {
    onThread();
    setMobileSidebarOpen(false);
    navigate({ view: "threads" });
    setRoute({ view: "threads" });
  }, [onThread]);

  const navigateToArtifacts = useCallback(() => {
    onThread();
    setMobileSidebarOpen(false);
    navigate({ view: "artifacts" });
    setRoute({ view: "artifacts" });
  }, [onThread]);

  const navigateToProjects = useCallback(() => {
    onThread();
    setMobileSidebarOpen(false);
    navigate({ view: "projects" });
    setRoute({ view: "projects" });
  }, [onThread]);

  const navigateToMemory = useCallback(() => {
    onThread();
    setMobileSidebarOpen(false);
    navigate({ view: "memory" });
    setRoute({ view: "memory" });
  }, [onThread]);

  const navigateToProject = useCallback(
    (project: Project) => {
      onThread();
      setMobileSidebarOpen(false);
      setOpenedProject(project);
      navigate({ view: "project", projectID: project.id });
      setRoute({ view: "project", projectID: project.id });
    },
    [onThread],
  );

  const {
    archivingProject,
    deletingProject,
    editingProject,
    isMutatingProject,
    openProjectDialog,
    setArchivingProject,
    setDeletingProject,
    setEditingProject,
    handleArchiveProjectConfirm,
    handleUnarchiveProject,
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

  function openArchiveProjectModal(project: Project) {
    setArchivingProject(project);
    setModalError("");
    setOpenThreadMenuID(null);
  }

  function unarchiveProjectAndReload(project: Project) {
    void handleUnarchiveProject(project).then(reloadThreads);
  }

  async function selectThread(threadID: string) {
    onThread();
    setMobileSidebarOpen(false);
    navigate({ view: "thread", threadID });
    setRoute({ view: "thread", threadID });
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
    clearStreamingBlocks();
    setSendError("");
    let abortController: AbortController | null = null;
    let createdThreadForFallback: Thread | null = null;
    let receivedThreadEvent = false;
    let keepFailedTurnVisible = false;
    // Id of the optimistic user bubble until the server confirms it; the catch reads
    // this to decide whether to drop the placeholder, so it must outlive the try block.
    let optimisticUserMessageID: string | null = null;
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
        // bound to it (project-less => private to this thread). Image uploads must
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
        navigate({ view: "thread", threadID: targetThread.id });
        setRoute({ view: "thread", threadID: targetThread.id });
      }
      const targetThreadID = targetThread.id;
      activeThreadIDRef.current = targetThreadID;
      abortController = new AbortController();
      streamAbortRef.current?.abort();
      streamAbortRef.current = abortController;
      setActiveStreamingThreadID(targetThreadID);
      const isCurrentThread = () => activeThreadIDRef.current === targetThreadID;
      // Accumulate this turn's ordered blocks in a closure-local array, the single
      // source of truth for the graft at turn end. The rendered state mirror is
      // kept in sync via setStreamingBlocks, but the React ref can be reset by
      // unrelated route effects mid-stream, so the graft must not depend on it.
      let liveBlocks: ContentBlock[] = [];
      const applyBlocks = (updater: (current: ContentBlock[]) => ContentBlock[]) => {
        liveBlocks = updater(liveBlocks);
        streamingBlocksRef.current = liveBlocks;
        setStreamingBlocks(liveBlocks);
      };
      const documentAttachmentIds = options.attachments
        .filter((attachment) => attachment.documentId !== undefined)
        .map((attachment) => attachment.documentId!);
      const imageAttachmentIds = options.attachments
        .filter((attachment) => isImageAttachment(attachment) && attachment.artifactId !== undefined)
        .map((attachment) => attachment.artifactId!);
      // Show the user's prompt immediately, before the stream's first event. The
      // server later echoes it as a `user_message` event, but on buffering networks
      // (e.g. a corporate proxy holding the whole SSE response) that event can be
      // delayed until the end, so without an optimistic bubble the prompt appears to
      // vanish on send. `onUserMessage` reconciles this temp message to the persisted
      // one by id; the catch removes it if the send never reached the server.
      if (isCurrentThread()) {
        // Avoid crypto.randomUUID: it is undefined in insecure contexts (plain http://),
        // which a corporate intranet deployment may well be — and that is exactly where
        // this fix matters. Date.now()+random is unique enough for a transient id.
        const tempID = `temp-user-${Date.now()}-${Math.random().toString(36).slice(2)}`;
        optimisticUserMessageID = tempID;
        const optimisticMessage: MessageWithActivityTrace = {
          id: tempID,
          clientKey: tempID,
          threadId: targetThreadID,
          role: "user",
          content,
          createdAt: new Date().toISOString(),
          ...(options.attachments.length > 0
            ? { attachments: options.attachments.map(toSentAttachment) }
            : {}),
        };
        setMessages((current) => [...current, optimisticMessage]);
      }
      await streamMessage(targetThreadID, content, {
        onUserMessage: (message) => {
          if (!isCurrentThread()) return;
          const confirmed =
            options.attachments.length > 0
              ? { ...message, attachments: options.attachments.map(toSentAttachment) }
              : message;
          // Fold the persisted message into the list, replacing the optimistic
          // placeholder in place (its clientKey/position survive => stable React key,
          // no remount or scroll jump). Capture the placeholder id into a const rather
          // than reading the outer `optimisticUserMessageID` inside the updater: the
          // latter is reset to null synchronously below, but React may defer the
          // updater (when its queue is non-empty mid-stream) until after that reset —
          // reading null then would miss the placeholder, append a second bubble, and
          // leave the orphaned optimistic one. Reset before setMessages so the catch
          // block treats the message as confirmed and won't drop it.
          const placeholderID = optimisticUserMessageID;
          optimisticUserMessageID = null;
          setMessages((current) => reconcileUserMessage(current, placeholderID, confirmed));
        },
        onDelta: (delta) => {
          // Each content delta extends the trailing text block, or opens a new one
          // when the trailing block is a trace/artifact — so prose that resumes
          // after a tool round becomes its own block, preserving chronology.
          applyBlocks((current) => appendTextDelta(current, delta));
        },
        onReasoningDelta: (delta) => {
          applyBlocks((current) => appendReasoningDeltaBlock(current, delta));
        },
        onReasoningTitle: (event) => {
          applyBlocks((current) => applyReasoningTitleBlock(current, event.id, event.title));
        },
        onToolPending: () => {
          setToolPending(true);
        },
        onToolCall: (event) => {
          // The pending call is now a real (running) trace event; let the trace's
          // own running status drive the "thinking" affordance from here.
          setToolPending(false);
          applyBlocks((current) => upsertToolCallBlock(current, event));
        },
        onToolResult: (event) => {
          applyBlocks((current) => upsertToolResultBlock(current, event));
        },
        onArtifact: (artifact) => {
          applyBlocks((current) => appendArtifactBlock(current, artifact));
        },
        onAssistantMessage: (message) => {
          // The persisted message may already carry the backend's ordered
          // contentBlocks. When it doesn't (older backends / lag), graft the
          // just-streamed blocks — settled to done — so the chronological order
          // (and the activity panel) survives the turn settling. The final answer
          // text can arrive only on the assistant_message (not as deltas), so
          // ensure the message content is represented as a trailing text block
          // when the streamed blocks carry no prose of their own.
          if (isCurrentThread()) {
            setMessages((current) => {
              const grafted = graftStreamedBlocks(message, liveBlocks);
              // Mirror the user-message dedup: if a route refresh already loaded this
              // assistant message, replace it in place (keeping the richer grafted
              // blocks and its clientKey) instead of appending a duplicate bubble.
              const index = current.findIndex((item) => item.id === grafted.id);
              if (index === -1) return [...current, grafted];
              const next = current.slice();
              next[index] = { ...grafted, clientKey: current[index].clientKey };
              return next;
            });
          }
          clearStreamingBlocks();
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
      }, abortController.signal, { documentAttachmentIds, imageAttachmentIds });
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
      // Keep the partial streamed blocks visible so a failed turn still shows what
      // streamed (prose, an activity trace, a tool that errored); the next send
      // clears them.
      keepFailedTurnVisible = true;
      // If the server never confirmed the user message (still the unreconciled
      // optimistic placeholder), drop it — the draft is restored below so the user
      // can retry, and a lingering sent-bubble with no reply would be misleading. A
      // placeholder already reconciled to a persisted message keeps its real id and
      // is left in place as part of the failed-but-visible turn.
      if (optimisticUserMessageID !== null) {
        const staleID = optimisticUserMessageID;
        setMessages((current) => current.filter((item) => item.id !== staleID));
      }
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
  const visibleStreamingBlocks = activeThreadOwnsStreamState ? streamingBlocks : [];
  const visibleToolPending = activeThreadOwnsStreamState ? toolPending : false;
  // Keep errors with the thread that owns the active or failed stream state.
  const visibleSendError = streamingThreadID === null || activeThreadOwnsStreamState ? sendError : "";

  return (
    <div
      className={`grid h-svh grid-rows-[minmax(0,1fr)] bg-bg font-sans text-ink transition-[grid-template-columns] duration-200 ease-out grid-cols-[1fr] ${
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
        onNewThread={navigateToNew}
        onThreads={navigateToThreads}
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
        onEditProject={openProjectDialog}
        onArchiveProject={openArchiveProjectModal}
        onDeleteProject={(project) => {
          setDeletingProject(project);
          setModalError("");
          setOpenThreadMenuID(null);
        }}
        onToggleThreadMenu={(menuKey) =>
          setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
        }
        onCloseThreadMenu={() => setOpenThreadMenuID(null)}
      />
      <main className="min-h-0 min-w-0 overflow-hidden bg-bg">
        {showAdmin ? (
          adminPanel
        ) : route.view === "threads" ? (
          <ThreadsPage
            mutationVersion={threadMutationVersion}
            projectsAvailable={projects.length > 0}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            onNewThread={navigateToNew}
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
            onUseInThread={handleUseArtifactInThread}
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
            onArchiveProject={openArchiveProjectModal}
            onUnarchiveProject={unarchiveProjectAndReload}
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
              loadError={loadError === "" && threadDataLoaded ? "Project not found." : loadError}
              onOpenSidebar={() => setMobileSidebarOpen(true)}
              onCreateProject={() => openProjectDialog(null)}
              onOpenProject={navigateToProject}
              onEditProject={openProjectDialog}
              onArchiveProject={openArchiveProjectModal}
              onUnarchiveProject={unarchiveProjectAndReload}
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
              onStarThread={(thread, starred, menuKey) => void handleSetThreadStarred(thread, starred, menuKey)}
              onRemoveFromProject={(thread) => void handleRemoveThreadFromProject(thread)}
              onToggleThreadMenu={(menuKey) =>
                setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
              }
              onCloseThreadMenu={() => setOpenThreadMenuID(null)}
              onEditProject={openProjectDialog}
              onArchiveProject={openArchiveProjectModal}
              onUnarchiveProject={unarchiveProjectAndReload}
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
          <ThreadPanel
            thread={activeThread}
            threadProject={activeThreadProject}
            deferredAttachNote={deferredAttachNote}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            messages={messages}
            draft={draft}
            streamingBlocks={visibleStreamingBlocks}
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
      {archivingProject !== null && (
        <ArchiveProjectModal
          project={archivingProject}
          error={modalError}
          disabled={isMutatingProject}
          onCancel={() => setArchivingProject(null)}
          onArchive={() => void handleArchiveProjectConfirm()}
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
