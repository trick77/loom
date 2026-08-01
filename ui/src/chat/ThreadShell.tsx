import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AuthExpiredError,
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  createThread,
  listThreads,
  setProjectStarred,
  setThreadStarred,
  stopMessage,
  streamMessage,
  streamIncognitoMessage,
  type Artifact,
  type Citation,
  type ContentBlock,
  type MessagePastedText,
  type Project,
  type ShareInfo,
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
import { SlashCommandPanel } from "./SlashCommandPanel";
import { matchSlashCommand, type SlashCommandName } from "./slashCommands";
import {
  createPastedText,
  pastedTextFromBlock,
  toPastedTextBlock,
  type PastedText,
} from "./pastedText";
import {
  clearDraft,
  composeContent,
  INCOGNITO_DRAFT_SCOPE,
  draftScopeKey,
  getDraft,
  setDraft as setScopedDraft,
  setDraftPastedTexts as setScopedPastedTexts,
  setDraftText as setScopedDraftText,
  threadDraftScope,
  type ComposerDrafts,
  type DraftScope,
} from "./composerDrafts";
import {
  INCOGNITO_RUN_KEY,
  isStreaming,
  selectRun,
  threadRunKey,
  type RunKey,
} from "./streamRuns";
import { useStreamRuns } from "./useStreamRuns";
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
import { IncognitoPanel } from "./IncognitoPanel";
import { Sidebar } from "./Sidebar";
import { tabTitle } from "./tabTitle";
import { DeleteThreadModal, RenameThreadModal } from "./threadModals";
import { SearchModal } from "./SearchModal";
import { ArchiveProjectModal } from "../projects/ArchiveProjectModal";
import { DeleteProjectModal } from "../projects/DeleteProjectModal";
import { ProjectDetailPage } from "../projects/ProjectDetailPage";
import { ProjectDialog } from "../projects/ProjectDialog";
import { ProjectPickerDialog } from "../projects/ProjectPickerDialog";
import { ProjectsPage } from "../projects/ProjectsPage";
import {
  replaceThreadById,
  upsertThreadById,
} from "../projects/projectMembership";
import { reconcileUserMessage, updateMessageAttachment } from "./threadUtils";
import { DEFAULT_REASONING_EFFORT, type ReasoningEffort } from "./reasoning";
import { isWithinUploadSizeLimit } from "./attachmentFiles";

export { buildImageStats } from "./artifacts";
export { GeneratedArtifactCard } from "./GeneratedArtifactCard";
export { ProseMarkdown } from "./messages";

// Each sources event is a full snapshot of *its own kind* only: knowledge_sources
// carries the user's numbered documents (no url) once before the model runs,
// web_sources carries the gathered pages (url) after every tool round. Replacing
// the whole list on either would drop the other kind — since documents became
// citable, the first search result would unresolve every document [n] pill
// mid-answer and shift the display numbering that is meant to be append-only.
// So each event replaces only its own kind. Documents lead: they hold the low
// indices, numbered before the tool loop.
function isWebCitation(citation: Citation): boolean {
  return typeof citation.url === "string" && citation.url !== "";
}

export function mergeSourceSnapshot(
  previous: Citation[],
  incoming: Citation[],
  incomingAreWeb: boolean,
): Citation[] {
  const kept = previous.filter(
    (citation) => isWebCitation(citation) !== incomingAreWeb,
  );
  return incomingAreWeb ? [...kept, ...incoming] : [...incoming, ...kept];
}

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
  const { t, i18n } = useTranslation();
  const [route, setRoute] = useState<RouteState>(() => routeFromLocation());
  // The textarea contents and the staged "Pasted" chips, keyed by the surface that
  // owns them (see composerDrafts.ts). They belong to the thread they were typed
  // in: leaving that thread must not carry them into the next one, and must not
  // throw them away either.
  const [drafts, setDrafts] = useState<ComposerDrafts>({});
  // Bumped whenever a retry loads a message back into the composer, to focus the
  // textarea and move the caret to the end (see Composer's focusSignal).
  const [composerFocusTick, setComposerFocusTick] = useState(0);
  // Files attached on the new-thread start screen, held until the first send creates
  // a thread to bind them to (deferred upload — avoids orphan empty threads and
  // scopes the upload to the thread it was attached in).
  const [pendingAttachments, setPendingAttachments] = useState<
    ComposerAttachment[]
  >([]);
  const [pendingAttachNote, setPendingAttachNote] = useState("");
  const [openThreadMenuID, setOpenThreadMenuID] = useState<string | null>(null);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [modalError, setModalError] = useState("");
  // Every assistant turn in flight, keyed by the thread that owns it (see
  // streamRuns.ts). Each run reconstructs its turn as a single ordered
  // ContentBlock[] (text / trace / artifact) mirroring the order the SSE events
  // arrive, so the transcript renders text, tool activity and images in true
  // chronological order; `sources` are the citations gathered so far, pushed
  // ahead of the deltas that cite them so inline [n] markers resolve while the
  // answer is still being written; `toolPending` bridges a model-yielded tool call
  // until its running trace event surfaces, driving the live "thinking"
  // affordance. Runs are independent, so several threads can stream at once — the
  // server has always allowed it (activeStreamRegistry is keyed by user+thread and
  // preempts rather than serializes), it was this state that did not.
  const {
    runs,
    begin: beginStreamRun,
    patch: patchStreamRun,
    rekey: rekeyStreamRun,
    end: endStreamRun,
    abort: abortStreamRun,
    abortAll: abortAllStreamRuns,
    nextProvisionalKey,
  } = useStreamRuns();
  // Incognito mode is a standalone, ephemeral chat reachable only from /new. Its
  // transcript lives entirely here and is never persisted or added to the thread
  // lists; exiting or leaving discards it. Its turn is just another run, under a
  // reserved key.
  const [incognito, setIncognito] = useState(false);
  const [incognitoMessages, setIncognitoMessages] = useState<
    MessageWithActivityTrace[]
  >([]);
  // The composer's reasoning-effort choice. In-memory only — it is not persisted
  // and does not carry across threads: every new thread opens at the default
  // (navigateToNew resets it), and a manual change applies to the current thread
  // only. High is the model's own default.
  const [reasoningEffort, setReasoningEffort] = useState<ReasoningEffort>(
    DEFAULT_REASONING_EFFORT,
  );
  // A slash command ("/mcp", "/tools", …) opens this ephemeral overlay panel
  // instead of sending a message; null when no panel is open.
  const [slashCommand, setSlashCommand] = useState<SlashCommandName | null>(
    null,
  );
  function setDraftText(scope: DraftScope, text: string) {
    setDrafts((current) => setScopedDraftText(current, scope, text));
  }
  // Large pastes collapsed into removable "Pasted" chips shown above the textarea.
  // Folded back into the outgoing message content on send (never uploaded/indexed).
  function handleAddPastedText(text: string) {
    setDrafts((current) =>
      setScopedPastedTexts(current, draftScope, [
        ...getDraft(current, draftScope).pastedTexts,
        createPastedText(text),
      ]),
    );
  }
  function handleRemovePastedText(id: string) {
    setDrafts((current) =>
      setScopedPastedTexts(
        current,
        draftScope,
        getDraft(current, draftScope).pastedTexts.filter(
          (pasted) => pasted.id !== id,
        ),
      ),
    );
  }
  // Flush hook for the deferred new-thread upload: the scope is supplied per call at
  // send time (the thread does not exist yet when the file is picked). Its
  // attachNote carries ingestion status/errors after the start screen is gone, so
  // it is surfaced in the thread panel the user lands on.
  const {
    attachNote: deferredAttachNote,
    uploadExistingAttachments: flushPendingAttachments,
  } = useDocumentAttachments({});
  // Errors that belong to the shell rather than to a turn (starring, attaching,
  // thread loading). A failed turn's own error lives on its run, so it stays with
  // the thread that failed. Report these through reportShellError below, not this
  // setter directly, so the newer of the two wins; the bare setter is for clearing.
  const [sendError, setSendError] = useState("");
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

  const handleActionError = useCallback(
    (error: unknown, fallback: string, setError: (message: string) => void) => {
      if (error instanceof AuthExpiredError) {
        onSessionExpired();
        return;
      }
      setError(
        error instanceof Error && error.message !== ""
          ? error.message
          : fallback,
      );
    },
    [onSessionExpired],
  );

  const {
    activeProject: activeProjectForRoute,
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
  } = useThreadData({
    abortAllStreamRuns,
    activeThreadIDRef,
    handleActionError,
    onSessionExpired,
  });

  // The composer surface currently on screen, and the draft that belongs to it.
  // Keyed off `activeThread`, not `route`: the route flips the instant a thread is
  // clicked while `activeThread` (and so the panel, its messages and the send
  // target) only follows once the fetch resolves. Reading the route here would
  // show the next thread's draft over the previous thread's transcript, and a send
  // in that window would post the next thread's draft to the previous thread.
  const draftScope = draftScopeKey(
    route.view === "thread" && activeThread !== null
      ? { view: "thread", threadID: activeThread.id }
      : route,
    incognito,
  );
  const draft = getDraft(drafts, draftScope);

  // The run the visible composer controls. Null on the start screen and on the
  // project page: a turn launched from either is rekeyed onto its new thread and
  // navigated to, so by the time there is something to stop, there is a thread.
  const activeRunKey: RunKey | null = incognito
    ? INCOGNITO_RUN_KEY
    : activeThread !== null
      ? threadRunKey(activeThread.id)
      : null;
  const activeRun = selectRun(runs, activeRunKey);
  const activeThreadIsStreaming = isStreaming(runs, activeRunKey);

  // Newest error wins. A failed turn's error stays pinned to its own thread, which
  // is what keeps it findable when you come back — but it has no dismissal of its
  // own, so without this it would shadow every later star / attach / load failure
  // on that thread indefinitely. Reporting a shell error clears it.
  const reportShellError = useCallback(
    (message: string) => {
      setSendError(message);
      if (message !== "" && activeRunKey !== null)
        patchStreamRun(activeRunKey, { error: "" });
    },
    [activeRunKey, patchStreamRun],
  );

  // Deliberately not gated on `runs`: that record changes on every delta, and a
  // dependency on it would re-create this callback — and so tear down and re-add
  // the Escape listener below — once per streamed token, per running thread.
  // Every caller already gates on the Stop control being rendered, and aborting a
  // key with no run is a no-op.
  const handleStopResponse = useCallback(
    (source = "stop_button") => {
      if (activeRunKey === null) return;
      const abort = () => abortStreamRun(activeRunKey);
      // Incognito has no server-side stop endpoint — dropping the fetch is the
      // whole mechanism there.
      if (incognito || activeThread === null) {
        abort();
        return;
      }
      // Tell the server which UI action stopped the stream, and only abort the
      // fetch once that stop request has been sent. Aborting first would drop the
      // connection and make the server log the generic request-context cancel
      // instead of this attributed one (the cancel cause is first-writer-wins).
      void stopMessage(activeThread.id, source)
        .catch((error: unknown) => {
          handleActionError(error, t("thread.stopFailed"), reportShellError);
        })
        .finally(abort);
    },
    [
      abortStreamRun,
      activeRunKey,
      activeThread,
      handleActionError,
      incognito,
      reportShellError,
      t,
    ],
  );

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

  // Escape stops the turn on the thread you are looking at. Runs on other threads
  // keep going — you stop those by opening them.
  useEffect(() => {
    if (!activeThreadIsStreaming) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.preventDefault();
      handleStopResponse("escape");
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [activeThreadIsStreaming, handleStopResponse]);

  // ⌘K / Ctrl-K opens the search palette from anywhere in the app.
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (
        (event.metaKey || event.ctrlKey) &&
        (event.key === "k" || event.key === "K")
      ) {
        event.preventDefault();
        setSearchOpen(true);
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, []);

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
          if (attachment.previewUrl !== undefined)
            URL.revokeObjectURL(attachment.previewUrl);
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
    (route.view === "project" && openedProject?.id === route.projectID
      ? openedProject
      : null);

  useEffect(() => {
    document.title = tabTitle(route, activeThread, activeProject);
  }, [route, activeThread?.title, activeProject?.name, i18n.language]);

  const navigateToNew = useCallback(() => {
    onThread();
    setMobileSidebarOpen(false);
    activeThreadIDRef.current = null;
    setActiveThread(null);
    setMessages([]);
    setSendError("");
    // Every new thread starts at the default reasoning effort; the previous
    // thread's choice does not carry over (it is never persisted).
    setReasoningEffort(DEFAULT_REASONING_EFFORT);
    navigate({ view: "new" });
    setRoute({ view: "new" });
  }, [onThread]);

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

  async function handleSetThreadStarred(
    thread: Thread,
    starred: boolean,
    menuKey?: string,
  ) {
    if (isUpdatingStar) return;
    setIsUpdatingStar(true);
    try {
      const updatedThread = await setThreadStarred(thread.id, starred);
      if (activeThreadIDRef.current === updatedThread.id) {
        setActiveThread(updatedThread);
      }
      setThreads((current) =>
        current.map((item) =>
          item.id === updatedThread.id ? updatedThread : item,
        ),
      );
      setProjectThreads((current) => replaceThreadById(current, updatedThread));
      setThreadMutationVersion((value) => value + 1);
      if (menuKey !== undefined) {
        setOpenThreadMenuID(null);
      }
      setSendError("");
    } catch (error) {
      handleActionError(error, t("thread.updateFailed"), reportShellError);
    } finally {
      setIsUpdatingStar(false);
    }
  }

  // Sharing/unsharing from the dialog updates activeShare, but the SharedPill in
  // the chat lists reads thread.shared — so mirror the new share state onto the
  // active thread in every list it appears in, otherwise the pill only updates
  // after a full reload.
  const handleShareChange = useCallback(
    (share: ShareInfo | null) => {
      setActiveShare(share);
      const id = activeThreadIDRef.current;
      if (id === null) return;
      const shared = share !== null && share.shared;
      setThreads((current) =>
        current.map((item) => (item.id === id ? { ...item, shared } : item)),
      );
      setProjectThreads((current) =>
        current.map((item) => (item.id === id ? { ...item, shared } : item)),
      );
    },
    [setActiveShare, setThreads, setProjectThreads],
  );

  async function handleSetProjectStarred(
    project: Project,
    starred: boolean,
    menuKey?: string,
  ) {
    if (isUpdatingStar) return;
    setIsUpdatingStar(true);
    try {
      const updatedProject = await setProjectStarred(project.id, starred);
      setProjects((current) =>
        current.map((item) =>
          item.id === updatedProject.id ? updatedProject : item,
        ),
      );
      if (menuKey !== undefined) {
        setOpenThreadMenuID(null);
      }
      setSendError("");
    } catch (error) {
      handleActionError(
        error,
        t("thread.projectUpdateFailed"),
        reportShellError,
      );
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
        setPendingAttachNote(
          `You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`,
        );
        return current;
      }
      const accepted = sizeFiltered.slice(0, remaining);
      if (accepted.length < sizeFiltered.length) {
        setPendingAttachNote(
          `You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`,
        );
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
      if (removed?.previewUrl !== undefined)
        URL.revokeObjectURL(removed.previewUrl);
      return current.filter((attachment) => attachment.id !== id);
    });
    setPendingAttachNote("");
  }

  async function handleSend(
    attachments: ComposerAttachment[] = pendingAttachments.map(
      toSentAttachment,
    ),
  ) {
    const draftText = draft.text.trim();
    const content = composeContent(draft);
    if (content === "") return;
    // Only this thread's own turn blocks a new send. Other threads streaming is
    // exactly what is now allowed.
    if (
      activeThread !== null &&
      isStreaming(runs, threadRunKey(activeThread.id))
    )
      return;
    // Slash command detection is the draft alone (the popover keys off it too); it
    // clears any staged chips along with the draft.
    if (runSlashCommand(draftText)) return;
    // sendContent clears this scope's draft and chips; on a send error it restores
    // the chips and the draft-only text (not the merged content) for retry.
    await sendContent(content, {
      restoreDraftOnError: true,
      attachments,
      restoreDraft: draftText,
      restorePastedTexts: draft.pastedTexts,
      pastedTexts: draft.pastedTexts,
      draftScope,
    });
  }

  // runSlashCommand intercepts a "/command" draft: it opens the ephemeral overlay
  // panel and clears the composer instead of sending a message to the LLM.
  // Returns true when the draft was a command (so callers stop).
  function runSlashCommand(content: string): boolean {
    const command = matchSlashCommand(content);
    if (command === null) return false;
    setDrafts((current) => clearDraft(current, draftScope));
    setSlashCommand(command.name);
    return true;
  }

  // Retry loads the message back into the composer for the user to edit and send
  // manually, rather than re-sending it immediately. Collapsed pastes are re-staged
  // as chips (not the folded inline text), so a resend keeps the same collapse.
  function handleRetry(content: string, pastedTexts?: MessagePastedText[]) {
    const blocks = pastedTexts ?? [];
    if ((content.trim() === "" && blocks.length === 0) || activeThread === null)
      return;
    setDrafts((current) =>
      setScopedDraft(current, draftScope, {
        text: content,
        pastedTexts: blocks.map(pastedTextFromBlock),
      }),
    );
    setComposerFocusTick((tick) => tick + 1);
  }

  async function sendContent(
    content: string,
    options: {
      restoreDraftOnError: boolean;
      attachments: ComposerAttachment[];
      // On error restore the textarea to this (the draft alone, without the merged
      // pasted blocks) and re-stage restorePastedTexts, so a large paste returns as
      // a chip rather than flooding the textarea. Defaults to the full `content`.
      restoreDraft?: string;
      restorePastedTexts?: PastedText[];
      // The collapsed paste blocks to send with this message: folded into `content`
      // for the model, and carried alongside so the sent bubble renders "Pasted"
      // chips instead of the inline wall of text (persisted server-side).
      pastedTexts?: PastedText[];
      // The composer this send came from. Its draft is cleared now and restored
      // here on error — see restoreScope below for the one case they differ.
      draftScope: DraftScope;
    },
  ) {
    // A turn on an existing thread is keyed by that thread. A turn started before
    // the thread exists takes a provisional key of its own, so a second send while
    // createThread (and possibly an image-upload flush) is still in flight cannot
    // land on the first one's run.
    let runKey: RunKey =
      activeThread !== null
        ? threadRunKey(activeThread.id)
        : nextProvisionalKey();
    // Where a failure puts the draft back: the thread that failed, not whichever
    // one happens to be on screen by then. A send that creates a thread retargets
    // this to the thread it landed on.
    let restoreScope: DraftScope = options.draftScope;
    setDrafts((current) => clearDraft(current, options.draftScope));
    setSendError("");
    const abortController = new AbortController();
    beginStreamRun(runKey, abortController);
    let createdThreadForFallback: Thread | null = null;
    let receivedThreadEvent = false;
    let keepFailedTurnVisible = false;
    // Id of the optimistic user bubble until the server confirms it; the catch reads
    // this to decide whether to drop the placeholder, so it must outlive the try block.
    let optimisticUserMessageID: string | null = null;
    // The thread this run belongs to, known up front for an existing thread and
    // filled in below for one created by this send. Whether the user is still
    // looking at it decides the writes into `messages`, which is a single array
    // for the thread on screen — run state is keyed and needs no such guard. A run
    // that settles while the user is elsewhere is picked up by loadRoute's refetch
    // on the way back.
    let targetThreadID: string | null = activeThread?.id ?? null;
    const isCurrentThread = () =>
      targetThreadID !== null && activeThreadIDRef.current === targetThreadID;
    // Captured once: the run outlives navigation, so a live `route` read inside
    // the stream callbacks would go stale.
    const projectIDForNewThread =
      route.view === "project" ? route.projectID : null;
    const updateSentAttachmentStatus = (
      id: string,
      patch: Partial<ComposerAttachment>,
    ) => {
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
            : await createThread({
                projectId: projectIDForNewThread,
                title: content,
              });
        createdThreadForFallback = targetThread;
        // Now that a thread exists, flush files attached on the start screen,
        // bound to it (project-less => private to this thread). Image uploads must
        // finish before the first model request so their artifact ids can be sent
        // as multimodal inputs; document indexing still continues in the background.
        const attachmentsToFlush = pendingAttachments;
        if (attachmentsToFlush.length > 0) {
          // Take the staged files off the start screen *before* the await, not
          // after: the start screen stays interactive for the whole (multi-second)
          // creation window now that sending no longer disables it, and a second
          // send from there would otherwise re-read this list and flush the same
          // files into its own thread — with updateSentAttachmentStatus rewriting
          // the shared attachment objects' artifact ids under this send's feet.
          // Not revoked, so the object URLs stay alive for the optimistic bubble.
          setPendingAttachments([]);
          await flushPendingAttachments(
            attachmentsToFlush,
            {
              threadId: targetThread.id,
              projectId: projectIDForNewThread ?? undefined,
            },
            updateSentAttachmentStatus,
          );
          const failedImageAttachment = options.attachments.find(
            (attachment) =>
              isImageAttachment(attachment) &&
              (attachment.status === "error" ||
                attachment.artifactId === undefined),
          );
          if (failedImageAttachment !== undefined) {
            // The send stops here with the start screen still on show, so put the
            // files back rather than making the user pick them again.
            setPendingAttachments(attachmentsToFlush);
            throw new Error(
              failedImageAttachment.error ??
                `Failed to upload ${failedImageAttachment.filename}.`,
            );
          }
        }
        setActiveThread(targetThread);
        activeThreadIDRef.current = targetThread.id;
        setMessages([]);
        // The run now belongs to a real thread. Rekeying in the same tick as the
        // route switch is what lets the thread we are about to land on pick the
        // turn up mid-flight.
        const createdRunKey = threadRunKey(targetThread.id);
        rekeyStreamRun(runKey, createdRunKey);
        runKey = createdRunKey;
        restoreScope = threadDraftScope(targetThread.id);
        navigate({ view: "thread", threadID: targetThread.id });
        setRoute({ view: "thread", threadID: targetThread.id });
      }
      targetThreadID = targetThread.id;
      activeThreadIDRef.current = targetThreadID;
      const threadIDForRun = targetThreadID;
      // Accumulate this turn's ordered blocks in a closure-local array, the single
      // source of truth for the graft at turn end. The rendered copy lives on the
      // run, but a run can be ended (or superseded) from elsewhere, so the graft
      // must not depend on reading it back.
      let liveBlocks: ContentBlock[] = [];
      const applyBlocks = (
        updater: (current: ContentBlock[]) => ContentBlock[],
      ) => {
        liveBlocks = updater(liveBlocks);
        patchStreamRun(runKey, { blocks: liveBlocks });
      };
      const documentAttachmentIds = options.attachments
        .filter((attachment) => attachment.documentId !== undefined)
        .map((attachment) => attachment.documentId!);
      const imageAttachmentIds = options.attachments
        .filter(
          (attachment) =>
            isImageAttachment(attachment) &&
            attachment.artifactId !== undefined,
        )
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
          threadId: threadIDForRun,
          role: "user",
          content,
          createdAt: new Date().toISOString(),
          ...(options.attachments.length > 0
            ? { attachments: options.attachments.map(toSentAttachment) }
            : {}),
          ...(options.pastedTexts && options.pastedTexts.length > 0
            ? { pastedTexts: options.pastedTexts.map(toPastedTextBlock) }
            : {}),
        };
        setMessages((current) => [...current, optimisticMessage]);
      }
      await streamMessage(
        threadIDForRun,
        content,
        {
          onUserMessage: (message) => {
            if (!isCurrentThread()) return;
            const confirmed =
              options.attachments.length > 0
                ? {
                    ...message,
                    attachments: options.attachments.map(toSentAttachment),
                  }
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
            setMessages((current) =>
              reconcileUserMessage(current, placeholderID, confirmed),
            );
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
            applyBlocks((current) =>
              applyReasoningTitleBlock(current, event.id, event.title),
            );
          },
          onToolPending: () => {
            patchStreamRun(runKey, { toolPending: true });
          },
          onToolCall: (event) => {
            // The pending call is now a real (running) trace event; let the trace's
            // own running status drive the "thinking" affordance from here.
            patchStreamRun(runKey, { toolPending: false });
            applyBlocks((current) => upsertToolCallBlock(current, event));
          },
          onToolResult: (event) => {
            applyBlocks((current) => upsertToolResultBlock(current, event));
          },
          onArtifact: (artifact) => {
            applyBlocks((current) => appendArtifactBlock(current, artifact));
          },
          // Each event is a full snapshot of one kind of source, so it replaces
          // that kind and leaves the other in place (see mergeSourceSnapshot).
          onWebSources: (sources) => {
            patchStreamRun(runKey, (run) => ({
              sources: mergeSourceSnapshot(run.sources, sources, true),
            }));
          },
          onKnowledgeSources: (sources) => {
            patchStreamRun(runKey, (run) => ({
              sources: mergeSourceSnapshot(run.sources, sources, false),
            }));
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
                const index = current.findIndex(
                  (item) => item.id === grafted.id,
                );
                if (index === -1) return [...current, grafted];
                const next = current.slice();
                next[index] = {
                  ...grafted,
                  clientKey: current[index].clientKey,
                };
                return next;
              });
            }
            // The settled message carries its own citations and blocks, so drop the
            // live copies now rather than at endRun — the stream reader yields
            // between chunks, so waiting would flash the turn twice.
            patchStreamRun(runKey, {
              blocks: [],
              sources: [],
              toolPending: false,
            });
          },
          onThread: (updatedThread) => {
            receivedThreadEvent = true;
            if (isCurrentThread()) setActiveThread(updatedThread);
            setThreads((current) => upsertThread(current, updatedThread));
            // Compare against the project captured when this send started, never a
            // live `route` read: a run outlives navigation now.
            if (
              projectIDForNewThread !== null &&
              updatedThread.projectId !== undefined &&
              updatedThread.projectId === projectIDForNewThread
            ) {
              setProjectThreads((current) =>
                upsertThreadById(current, updatedThread),
              );
            }
          },
        },
        abortController.signal,
        {
          documentAttachmentIds,
          imageAttachmentIds,
          reasoningEffort,
          pastedTexts: (options.pastedTexts ?? []).map(toPastedTextBlock),
        },
      );
      const fallbackThread = createdThreadForFallback;
      if (!receivedThreadEvent && fallbackThread !== null) {
        setThreads((current) => upsertThread(current, fallbackThread));
        if (
          projectIDForNewThread !== null &&
          fallbackThread.projectId !== undefined &&
          fallbackThread.projectId === projectIDForNewThread
        ) {
          setProjectThreads((current) =>
            upsertThreadById(current, fallbackThread),
          );
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
      // Guarded on the active thread for the same reason the placeholder was only
      // added there: `messages` holds whichever thread is on screen.
      if (optimisticUserMessageID !== null && isCurrentThread()) {
        const staleID = optimisticUserMessageID;
        setMessages((current) => current.filter((item) => item.id !== staleID));
      }
      if (options.restoreDraftOnError) {
        setDrafts((current) =>
          setScopedDraft(current, restoreScope, {
            text: options.restoreDraft ?? content,
            pastedTexts: options.restorePastedTexts ?? [],
          }),
        );
      }
      // A turn that reached a thread keeps its error on that thread, so it is
      // still there when you come back to it. One that failed before the thread
      // existed (createThread itself, or the deferred upload flush) has no thread
      // to pin it to and no surface showing that run — it belongs to the shell,
      // which is the start screen the user is still looking at.
      handleActionError(error, "Message failed to send.", (message) => {
        if (targetThreadID === null) reportShellError(message);
        else patchStreamRun(runKey, { error: message });
      });
    } finally {
      endStreamRun(runKey, {
        keepFailedTurnVisible: keepFailedTurnVisible && targetThreadID !== null,
        controller: abortController,
      });
    }
  }

  const enterIncognito = useCallback(() => {
    // Incognito starts clean: it takes over the whole surface, so it gets its own
    // draft scope and its own run key rather than borrowing the start screen's.
    // Normal threads keep streaming behind it — they are separate runs, and
    // nothing about them is visible or reachable from here.
    abortStreamRun(INCOGNITO_RUN_KEY);
    endStreamRun(INCOGNITO_RUN_KEY, { keepFailedTurnVisible: false });
    setDrafts((current) => clearDraft(current, INCOGNITO_DRAFT_SCOPE));
    setSendError("");
    setIncognitoMessages([]);
    setIncognito(true);
  }, [abortStreamRun, endStreamRun]);

  const exitIncognito = useCallback(() => {
    // Discard the ephemeral transcript — nothing was ever written, so there is
    // nothing to clean up server-side.
    abortStreamRun(INCOGNITO_RUN_KEY);
    endStreamRun(INCOGNITO_RUN_KEY, { keepFailedTurnVisible: false });
    setIncognito(false);
    setIncognitoMessages([]);
    setDrafts((current) => clearDraft(current, INCOGNITO_DRAFT_SCOPE));
    setSendError("");
  }, [abortStreamRun, endStreamRun]);

  // sendIncognitoContent mirrors sendContent's live-block accumulation but routes
  // to the stateless endpoint: no thread is created, no navigation happens, and the
  // whole prior transcript is replayed as history (the server keeps none). The
  // assistant message is appended to the in-memory transcript only.
  async function sendIncognitoContent(
    content: string,
    restoreDraftOnError: boolean,
    // On error restore the textarea to restore.draft (without the merged pasted
    // blocks) and re-stage restore.pastedTexts. Defaults to the full `content`.
    restore?: { draft: string; pastedTexts: PastedText[] },
  ) {
    setDrafts((current) => clearDraft(current, INCOGNITO_DRAFT_SCOPE));
    setSendError("");
    const history = incognitoMessages
      .filter(
        (message) => message.role === "user" || message.role === "assistant",
      )
      .map((message) => ({
        role: message.role as "user" | "assistant",
        content: message.content,
      }));
    const tempID = `incognito-user-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    const optimisticMessage: MessageWithActivityTrace = {
      id: tempID,
      clientKey: tempID,
      threadId: "incognito",
      role: "user",
      content,
      createdAt: new Date().toISOString(),
      // Render collapsed pastes as chips here too (incognito is ephemeral, so this
      // is the in-session bubble only), matching the persisted path in sendContent.
      ...(restore && restore.pastedTexts.length > 0
        ? { pastedTexts: restore.pastedTexts.map(toPastedTextBlock) }
        : {}),
    };
    setIncognitoMessages((current) => [...current, optimisticMessage]);
    const abortController = new AbortController();
    beginStreamRun(INCOGNITO_RUN_KEY, abortController);
    let liveBlocks: ContentBlock[] = [];
    const applyBlocks = (
      updater: (current: ContentBlock[]) => ContentBlock[],
    ) => {
      liveBlocks = updater(liveBlocks);
      patchStreamRun(INCOGNITO_RUN_KEY, { blocks: liveBlocks });
    };
    let keepFailedTurnVisible = false;
    try {
      await streamIncognitoMessage(
        content,
        history,
        {
          onUserMessage: () => {
            // The incognito endpoint does not echo the user message; the optimistic
            // bubble is the permanent one.
          },
          onDelta: (delta) =>
            applyBlocks((current) => appendTextDelta(current, delta)),
          onReasoningDelta: (delta) =>
            applyBlocks((current) => appendReasoningDeltaBlock(current, delta)),
          onReasoningTitle: (event) =>
            applyBlocks((current) =>
              applyReasoningTitleBlock(current, event.id, event.title),
            ),
          onAssistantMessage: (message) => {
            // Give each turn a unique id so React keys and per-message actions never
            // collide (the server returns a constant synthetic id).
            const uniqueID = `incognito-assistant-${Date.now()}-${Math.random().toString(36).slice(2)}`;
            const grafted = graftStreamedBlocks(
              { ...message, id: uniqueID },
              liveBlocks,
            );
            setIncognitoMessages((current) => [
              ...current,
              { ...grafted, clientKey: uniqueID },
            ]);
            patchStreamRun(INCOGNITO_RUN_KEY, {
              blocks: [],
              sources: [],
              toolPending: false,
            });
          },
          onThread: () => {
            // Incognito never emits a thread event; nothing to reconcile.
          },
        },
        abortController.signal,
        { reasoningEffort },
      );
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      keepFailedTurnVisible = true;
      // Drop the optimistic user bubble that never got a reply so the user can retry.
      setIncognitoMessages((current) =>
        current.filter((message) => message.id !== tempID),
      );
      if (restoreDraftOnError) {
        setDrafts((current) =>
          setScopedDraft(current, INCOGNITO_DRAFT_SCOPE, {
            text: restore?.draft ?? content,
            pastedTexts: restore?.pastedTexts ?? [],
          }),
        );
      }
      handleActionError(error, "Message failed to send.", (message) =>
        patchStreamRun(INCOGNITO_RUN_KEY, { error: message }),
      );
    } finally {
      endStreamRun(INCOGNITO_RUN_KEY, {
        keepFailedTurnVisible,
        controller: abortController,
      });
    }
  }

  async function handleIncognitoSend() {
    const draftText = draft.text.trim();
    const content = composeContent(draft);
    if (content === "" || isStreaming(runs, INCOGNITO_RUN_KEY)) return;
    if (runSlashCommand(draftText)) return;
    await sendIncognitoContent(content, true, {
      draft: draftText,
      pastedTexts: draft.pastedTexts,
    });
  }

  function handleIncognitoRetry(
    content: string,
    pastedTexts?: MessagePastedText[],
  ) {
    const blocks = pastedTexts ?? [];
    if (content.trim() === "" && blocks.length === 0) return;
    setDrafts((current) =>
      setScopedDraft(current, INCOGNITO_DRAFT_SCOPE, {
        text: content,
        pastedTexts: blocks.map(pastedTextFromBlock),
      }),
    );
    setComposerFocusTick((tick) => tick + 1);
  }

  // A failed turn's error belongs to its own thread; everything else (starring,
  // attaching, loading) belongs to the shell and shows wherever you are. The turn
  // error takes precedence only because reportShellError clears it first — so what
  // this really resolves to is whichever error happened most recently.
  const visibleSendError = activeRun.error !== "" ? activeRun.error : sendError;

  // Incognito takes over the whole surface with no sidebar or modals — it is a
  // self-contained, ephemeral view reachable only from the /new start screen.
  if (incognito) {
    return (
      <div className="grid h-svh grid-rows-[minmax(0,1fr)] grid-cols-[1fr] bg-bg font-sans text-ink">
        <main className="min-h-0 min-w-0 overflow-hidden bg-bg">
          <IncognitoPanel
            messages={incognitoMessages}
            draft={draft.text}
            streamingBlocks={activeRun.blocks}
            isSending={activeThreadIsStreaming}
            sendError={visibleSendError}
            reasoningEffort={reasoningEffort}
            onReasoningEffortChange={setReasoningEffort}
            onDraftChange={(text) => setDraftText(draftScope, text)}
            pastedTexts={draft.pastedTexts}
            onAddPastedText={handleAddPastedText}
            onRemovePastedText={handleRemovePastedText}
            onSend={() => void handleIncognitoSend()}
            onStop={() => handleStopResponse("incognito")}
            onRetry={handleIncognitoRetry}
            focusSignal={composerFocusTick}
            onExit={exitIncognito}
          />
        </main>
        {slashCommand !== null && (
          <SlashCommandPanel
            command={slashCommand}
            onClose={() => setSlashCommand(null)}
          />
        )}
      </div>
    );
  }

  return (
    <div
      className={`grid h-svh grid-rows-[minmax(0,1fr)] bg-bg font-sans text-ink transition-[grid-template-columns] duration-200 ease-out grid-cols-[1fr] ${
        sidebarCollapsed
          ? "md:grid-cols-[56px_1fr]"
          : "md:grid-cols-[362px_1fr]"
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
        onOpenSearch={() => setSearchOpen(true)}
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
          setOpenThreadMenuID((current) =>
            current === menuKey ? null : menuKey,
          )
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
            onStarThread={(thread, starred, menuKey) =>
              void handleSetThreadStarred(thread, starred, menuKey)
            }
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
              loadError={
                loadError === "" && threadDataLoaded
                  ? "Project not found."
                  : loadError
              }
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
              draft={draft.text}
              sendError={visibleSendError}
              isSending={false}
              sendDisabled={false}
              openThreadMenuID={openThreadMenuID}
              reasoningEffort={reasoningEffort}
              onReasoningEffortChange={setReasoningEffort}
              onBack={navigateToProjects}
              onDraftChange={(text) => setDraftText(draftScope, text)}
              pastedTexts={draft.pastedTexts}
              onAddPastedText={handleAddPastedText}
              onRemovePastedText={handleRemovePastedText}
              onSend={handleSend}
              onStop={handleStopResponse}
              onOpenThread={(threadID) => void selectThread(threadID)}
              onRenameThread={openRenameModal}
              onDeleteThread={openDeleteModal}
              onStarThread={(thread, starred, menuKey) =>
                void handleSetThreadStarred(thread, starred, menuKey)
              }
              onRemoveFromProject={(thread) =>
                void handleRemoveThreadFromProject(thread)
              }
              onToggleThreadMenu={(menuKey) =>
                setOpenThreadMenuID((current) =>
                  current === menuKey ? null : menuKey,
                )
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
              onToggleStar={(project, starred) =>
                void handleSetProjectStarred(project, starred)
              }
              onOpenSidebar={() => setMobileSidebarOpen(true)}
            />
          )
        ) : route.view === "new" ? (
          <StartPanel
            displayName={displayName}
            draft={draft.text}
            isSending={false}
            sendDisabled={false}
            sendError={visibleSendError}
            attachments={pendingAttachments}
            attachNote={pendingAttachNote}
            reasoningEffort={reasoningEffort}
            onReasoningEffortChange={setReasoningEffort}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            onDraftChange={(text) => setDraftText(draftScope, text)}
            pastedTexts={draft.pastedTexts}
            onAddPastedText={handleAddPastedText}
            onRemovePastedText={handleRemovePastedText}
            onSend={handleSend}
            onStop={handleStopResponse}
            onAttachFiles={handleAttachPendingFiles}
            onAttachError={setPendingAttachNote}
            onRemoveAttachment={handleRemovePendingAttachment}
            onEnterIncognito={enterIncognito}
          />
        ) : (
          <ThreadPanel
            thread={activeThread}
            threadProject={activeThreadProject}
            share={activeShare}
            onShareChange={handleShareChange}
            deferredAttachNote={deferredAttachNote}
            onOpenSidebar={() => setMobileSidebarOpen(true)}
            messages={messages}
            draft={draft.text}
            streamingBlocks={activeRun.blocks}
            streamingSources={activeRun.sources}
            toolPending={activeRun.toolPending}
            sendError={visibleSendError}
            isSending={activeThreadIsStreaming}
            sendDisabled={false}
            openThreadMenuID={openThreadMenuID}
            reasoningEffort={reasoningEffort}
            onReasoningEffortChange={setReasoningEffort}
            onDraftChange={(text) => setDraftText(draftScope, text)}
            pastedTexts={draft.pastedTexts}
            onAddPastedText={handleAddPastedText}
            onRemovePastedText={handleRemovePastedText}
            onSend={handleSend}
            onStop={handleStopResponse}
            onRetry={handleRetry}
            focusSignal={composerFocusTick}
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
            onStarThread={(thread, starred, menuKey) =>
              void handleSetThreadStarred(thread, starred, menuKey)
            }
            onToggleThreadMenu={(menuKey) =>
              setOpenThreadMenuID((current) =>
                current === menuKey ? null : menuKey,
              )
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
          onSelect={(project) =>
            void handleMoveThreadsToProject(movingThreads, project)
          }
        />
      )}
      {settingsOpen && <SettingsModal onClose={() => setSettingsOpen(false)} />}
      {slashCommand !== null && (
        <SlashCommandPanel
          command={slashCommand}
          onClose={() => setSlashCommand(null)}
        />
      )}
      {searchOpen && (
        <SearchModal
          onClose={() => setSearchOpen(false)}
          onSelectThread={(threadID) => void selectThread(threadID)}
        />
      )}
    </div>
  );
}

function upsertThread(current: Thread[], thread: Thread): Thread[] {
  return [thread, ...current.filter((item) => item.id !== thread.id)];
}
