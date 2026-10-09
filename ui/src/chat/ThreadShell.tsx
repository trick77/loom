import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";

import { UserFacingError } from "../api/http";
import { describeActionError } from "./actionErrors";
import {
  DEFAULT_THREAD_TITLE,
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  createThread,
  getThread,
  setProjectStarred,
  setThreadStarred,
  stopMessage,
  streamMessage,
  type Artifact,
  type ContentBlock,
  type Message,
  type MessagePastedText,
  type Project,
  type ShareInfo,
  type Thread,
  type User,
  PayloadTooLargeError,
  StreamInterruptedError,
} from "../api";
import { graftStreamedBlocks } from "./contentBlocks";
import { ThreadsPage } from "../chats/ThreadsPage";
import { useRouteState } from "./useRouteState";
import type { MessageWithActivityTrace } from "./types";
import { SlashCommandPanel } from "./SlashCommandPanel";
import { matchSlashCommand, type SlashCommandName } from "./slashCommands";
import { toPastedTextBlock, type PastedText } from "./pastedText";
import {
  clearDraft,
  composeContent,
  draftScopeKey,
  getDraft,
  isEmptyMessage,
  restagedDrafts,
  setDraft as setScopedDraft,
  threadDraftScope,
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
import { useIncognitoChat } from "./useIncognitoChat";
import { useShellChrome } from "./useShellChrome";
import {
  composerAttachmentFromArtifact,
  createComposerAttachment,
  isImageAttachment,
  toSentAttachment,
  useDocumentAttachments,
  type ComposerAttachment,
} from "./useDocumentAttachments";
import { rehydrateLoadedMessage, useThreadData } from "./useThreadData";
import { followRunningTurn, whenReachable } from "./followRunningTurn";
import { useProjectActions } from "./useProjectActions";
import { useThreadActions } from "./useThreadActions";
import { ThreadPanel } from "./ThreadPanel";
import { StartPanel } from "./StartPanel";
import { IncognitoPanel } from "./IncognitoPanel";
import { Sidebar } from "./Sidebar";
import { SidebarResizer } from "./SidebarResizer";
import { tabTitle } from "./tabTitle";
import { DeleteThreadModal, RenameThreadModal } from "./threadModals";
import { SearchModal } from "./SearchModal";
import { ArchiveProjectModal } from "../projects/ArchiveProjectModal";
import { DeleteProjectModal } from "../projects/DeleteProjectModal";
import { ProjectDetailPage } from "../projects/ProjectDetailPage";
import { ProjectDialog } from "../projects/ProjectDialog";
import { ProjectPickerDialog } from "../projects/ProjectPickerDialog";
import {
  replaceThreadById,
  upsertThreadById,
} from "../projects/projectMembership";
import { reconcileUserMessage, updateMessageAttachment } from "./threadUtils";
import { isWithinUploadSizeLimit } from "./attachmentFiles";
import { useComposerDrafts } from "./useComposerDrafts";
import { applyMessageCost } from "../metrics";
import { createTurnHandlers, newTempID } from "./turnHandlers";

// The secondary views and the settings modal load on first use rather than
// with the chat: a user who never opens the artifact library never pays for it.
const ArtifactsPage = lazy(() =>
  import("../artifacts/ArtifactsPage").then((module) => ({
    default: module.ArtifactsPage,
  })),
);
const MemoryPage = lazy(() =>
  import("../MemoryPage").then((module) => ({ default: module.MemoryPage })),
);
const ProjectsPage = lazy(() =>
  import("../projects/ProjectsPage").then((module) => ({
    default: module.ProjectsPage,
  })),
);
const SettingsModal = lazy(() =>
  import("../settings/SettingsModal").then((module) => ({
    default: module.SettingsModal,
  })),
);
import { useEscapeKey } from "./useEscapeKey";

// How long a stopped stream may take to deliver the server's saved partial
// before the fetch is dropped anyway. Saving waits up to 5s on reasoning
// titles, so this sits well past that.
const STOP_ABORT_FALLBACK_MS = 15_000;

// foldAssistantMessage puts a finished assistant message into the transcript.
// The persisted message may already carry the backend's ordered contentBlocks.
// When it doesn't (older backends / lag), the just-streamed blocks are grafted
// on — settled to done — so the chronological order (and the activity panel)
// survives the turn settling. A message a route refresh already loaded is
// replaced in place, keeping the richer grafted blocks and its clientKey,
// instead of appending a duplicate bubble.
function foldAssistantMessage(
  current: MessageWithActivityTrace[],
  message: Message,
  liveBlocks: ContentBlock[],
): MessageWithActivityTrace[] {
  const grafted = graftStreamedBlocks(message, liveBlocks);
  const index = current.findIndex((item) => item.id === grafted.id);
  if (index === -1) return [...current, grafted];
  const next = current.slice();
  next[index] = { ...grafted, clientKey: current[index].clientKey };
  return next;
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
  const { route, routeRef, go } = useRouteState();
  // The textarea contents and the staged "Pasted" chips, keyed by the surface that
  // owns them (see composerDrafts.ts). They belong to the thread they were typed
  // in: leaving that thread must not carry them into the next one, and must not
  // throw them away either.
  const {
    drafts,
    setDrafts,
    setDraftText,
    addPastedText,
    removePastedText,
    focusTick: composerFocusTick,
    requestFocus: requestComposerFocus,
  } = useComposerDrafts();
  // Files attached on the new-thread start screen, held until the first send creates
  // a thread to bind them to (deferred upload — avoids orphan empty threads and
  // scopes the upload to the thread it was attached in).
  const [pendingAttachments, setPendingAttachments] = useState<
    ComposerAttachment[]
  >([]);
  const pendingAttachmentCountRef = useRef(pendingAttachments.length);
  useEffect(() => {
    pendingAttachmentCountRef.current = pendingAttachments.length;
  }, [pendingAttachments.length]);
  const [pendingAttachNote, setPendingAttachNote] = useState("");
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
    has: hasStreamRun,
    abort: abortStreamRun,
    abortAll: abortAllStreamRuns,
    markStopRequested,
    stopRequested,
    nextProvisionalKey,
  } = useStreamRuns();
  // A slash command ("/mcp", "/tools", …) opens this ephemeral overlay panel
  // instead of sending a message; null when no panel is open.
  const [slashCommand, setSlashCommand] = useState<SlashCommandName | null>(
    null,
  );
  function handleAddPastedText(text: string) {
    addPastedText(draftScope, text);
  }
  function handleRemovePastedText(id: string) {
    removePastedText(draftScope, id);
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
  // Sidebar, menus and overlays (see useShellChrome.ts).
  const {
    openThreadMenuID,
    toggleThreadMenu,
    closeThreadMenu,
    userMenuOpen,
    toggleUserMenu,
    closeUserMenu,
    settingsOpen,
    openSettings,
    closeSettings,
    searchOpen,
    openSearch,
    closeSearch,
    isMobile,
    sidebarCollapsed,
    railCollapsed,
    toggleDesktopCollapsed,
    mobileSidebarOpen,
    openMobileSidebar,
    closeMobileSidebar,
  } = useShellChrome();
  const [threadMutationVersion, setThreadMutationVersion] = useState(0);
  const activeThreadIDRef = useRef<string | null>(null);
  // Set further down, once the state it closes over exists; read only from the
  // thread load, which runs after the first render.
  const attachToRunningTurnRef = useRef<(thread: Thread) => void>(() => {});

  // translateStreamError names the two transport failures the user can act on
  // in their own language; every other error keeps its own text (a server
  // error event is written for the user, an API failure is mapped by
  // handleActionError).
  const translateStreamError = useCallback(
    (error: unknown): unknown => {
      if (error instanceof StreamInterruptedError) {
        return new UserFacingError(t("thread.streamInterrupted"));
      }
      if (error instanceof PayloadTooLargeError) {
        return new UserFacingError(t("thread.messageTooLarge"));
      }
      return error;
    },
    [t],
  );

  const handleActionError = useCallback(
    (error: unknown, fallback: string, setError: (message: string) => void) => {
      const message = describeActionError(error, fallback);
      if (message === null) {
        onSessionExpired();
        return;
      }
      setError(message);
    },
    [onSessionExpired],
  );

  // Incognito mode: a standalone, ephemeral chat reachable only from /new. Its
  // turn is just another run, under a reserved key (see useIncognitoChat.ts).
  const {
    incognito,
    incognitoMessages,
    enterIncognito,
    exitIncognito,
    sendIncognitoContent,
    handleIncognitoRetry,
  } = useIncognitoChat({
    runs,
    beginStreamRun,
    patchStreamRun,
    endStreamRun,
    abortStreamRun,
    setDrafts,
    requestComposerFocus,
    setSendError,
    handleActionError,
    translateStreamError,
  });

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
    reloadThreads,
    setActiveThread,
    setMessages,
    setProjectThreads,
    setProjects,
    setThreads,
    starredProjects,
    starredThreads,
    unstarredProjects,
  } = useThreadData({
    abortAllStreamRuns,
    activeThreadIDRef,
    handleActionError,
    onSessionExpired,
    onRunningTurn: useCallback(
      (thread: Thread) => attachToRunningTurnRef.current(thread),
      [],
    ),
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
      // Tell the server which UI action stopped the stream, and do not abort the
      // fetch first: that would drop the connection and make the server log the
      // generic request-context cancel instead of this attributed one (the cancel
      // cause is first-writer-wins). Once stopped, the server saves the partial
      // answer and ends the stream with assistant_message and done; reading on
      // until then keeps the answer on screen. The abort is only a fallback for a
      // stream that never ends, or the stop itself when the server had no stream
      // registered yet. The run's catch reads this mark so a close is not
      // reported as a dropped connection.
      const controller = markStopRequested(activeRunKey);
      void stopMessage(activeThread.id, source).then(
        (stopped) => {
          if (!stopped) {
            abort();
            return;
          }
          window.setTimeout(() => controller?.abort(), STOP_ABORT_FALLBACK_MS);
        },
        (error: unknown) => {
          handleActionError(error, t("thread.stopFailed"), reportShellError);
          abort();
        },
      );
    },
    [
      abortStreamRun,
      activeRunKey,
      activeThread,
      handleActionError,
      incognito,
      markStopRequested,
      reportShellError,
      t,
    ],
  );

  // Escape goes to the most recently activated handler: the thread menu opens
  // inside an already open mobile drawer, so it closes first. Declaration order
  // only breaks a tie when both activate in the same commit.
  useEscapeKey(closeMobileSidebar, {
    active: mobileSidebarOpen,
  });
  useEscapeKey(closeThreadMenu, {
    active: openThreadMenuID !== null,
  });

  // Escape stops the turn on the thread you are looking at. Runs on other threads
  // keep going — you stop those by opening them.
  // Registered at the bottom of the Escape stack: any surface opened on top
  // (a lightbox, a dialog, a menu) takes the key first, so closing it never
  // also stops the answer.
  useEscapeKey(() => handleStopResponse("escape"), {
    active: activeThreadIsStreaming,
    bottom: true,
  });

  // ⌘K / Ctrl-K opens the search palette from anywhere in the app.
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (
        (event.metaKey || event.ctrlKey) &&
        (event.key === "k" || event.key === "K")
      ) {
        event.preventDefault();
        openSearch();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [openSearch]);

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
    closeMobileSidebar();
    activeThreadIDRef.current = null;
    setActiveThread(null);
    setMessages([]);
    setSendError("");
    go({ view: "new" });
  }, [go, onThread, setActiveThread, setMessages, closeMobileSidebar]);

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
    closeMobileSidebar();
    go({ view: "threads" });
  }, [go, onThread, closeMobileSidebar]);

  const navigateToArtifacts = useCallback(() => {
    onThread();
    closeMobileSidebar();
    go({ view: "artifacts" });
  }, [go, onThread, closeMobileSidebar]);

  const navigateToProjects = useCallback(() => {
    onThread();
    closeMobileSidebar();
    go({ view: "projects" });
  }, [go, onThread, closeMobileSidebar]);

  const navigateToMemory = useCallback(() => {
    onThread();
    closeMobileSidebar();
    go({ view: "memory" });
  }, [go, onThread, closeMobileSidebar]);

  const navigateToProject = useCallback(
    (project: Project) => {
      onThread();
      closeMobileSidebar();
      setOpenedProject(project);
      go({ view: "project", projectID: project.id });
    },
    [go, onThread, closeMobileSidebar],
  );

  const {
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
  } = useProjectActions({
    route,
    navigateToProject,
    navigateToProjects,
    setModalError,
    closeThreadMenu,
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
    openMoveModal,
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
    closeThreadMenu,
    setProjectThreads,
    setThreadMutationVersion,
    setThreads,
    handleActionError,
    onActiveThreadArchived: navigateToNew,
    onOpenThreadModal: closeMobileSidebar,
    route,
  });

  const openMoveThreadModal = useCallback(
    (thread: Thread) => openMoveModal([thread]),
    [openMoveModal],
  );

  function unarchiveProjectAndReload(project: Project) {
    void handleUnarchiveProject(project).then(reloadThreads);
  }

  const selectThread = useCallback(
    async (threadID: string) => {
      onThread();
      closeMobileSidebar();
      go({ view: "thread", threadID });
    },
    [go, onThread, closeMobileSidebar],
  );

  const handleSetThreadStarred = useCallback(
    async (thread: Thread, starred: boolean, menuKey?: string) => {
      if (isUpdatingStar) return;
      setIsUpdatingStar(true);
      try {
        const updatedThread = await setThreadStarred(thread.id, starred);
        if (activeThreadIDRef.current === updatedThread.id) {
          setActiveThread(updatedThread);
        }
        setThreads((current) => replaceThreadById(current, updatedThread));
        setProjectThreads((current) =>
          replaceThreadById(current, updatedThread),
        );
        setThreadMutationVersion((value) => value + 1);
        if (menuKey !== undefined) {
          closeThreadMenu();
        }
        setSendError("");
      } catch (error) {
        handleActionError(error, t("thread.updateFailed"), reportShellError);
      } finally {
        setIsUpdatingStar(false);
      }
    },
    [
      handleActionError,
      isUpdatingStar,
      reportShellError,
      setActiveThread,
      closeThreadMenu,
      setProjectThreads,
      setThreads,
      t,
    ],
  );

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

  const handleSetProjectStarred = useCallback(
    async (project: Project, starred: boolean, menuKey?: string) => {
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
          closeThreadMenu();
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
    },
    [
      handleActionError,
      isUpdatingStar,
      reportShellError,
      closeThreadMenu,
      setProjects,
      t,
    ],
  );

  function handleAttachPendingFiles(files: File[]) {
    setSendError("");
    const sizeFiltered = files.filter(isWithinUploadSizeLimit);
    if (sizeFiltered.length < files.length) {
      setPendingAttachNote(t("errors.fileTooLarge"));
    }
    // The count is mirrored in a ref so two drops in one render both see the
    // other's files, and the note is set here rather than inside the state
    // updater, which StrictMode runs twice.
    const remaining =
      DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE - pendingAttachmentCountRef.current;
    if (remaining <= 0) {
      setPendingAttachNote(
        t("composer.attachLimit", {
          count: DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
        }),
      );
      return;
    }
    const accepted = sizeFiltered.slice(0, remaining);
    if (accepted.length < sizeFiltered.length) {
      setPendingAttachNote(
        t("composer.attachLimit", {
          count: DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
        }),
      );
    } else if (accepted.length > 0 && sizeFiltered.length === files.length) {
      setPendingAttachNote("");
    }
    if (accepted.length === 0) return;
    pendingAttachmentCountRef.current += accepted.length;
    const queued = accepted.map((file) =>
      createComposerAttachment(file, "queued"),
    );
    setPendingAttachments((current) => [...current, ...queued]);
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
  // Stable across renders: it reaches every message bubble, and the shell
  // re-renders on each keystroke and streamed token, so a fresh identity here
  // would defeat the bubbles' memo.
  const hasActiveThread = activeThread !== null;
  const handleRetry = useCallback(
    (content: string, pastedTexts?: MessagePastedText[]) => {
      if (!hasActiveThread || isEmptyMessage(content, pastedTexts)) return;
      setDrafts(
        (current) =>
          restagedDrafts(current, draftScope, content, pastedTexts) ?? current,
      );
      requestComposerFocus();
    },
    [draftScope, hasActiveThread, requestComposerFocus, setDrafts],
  );

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
    // The server stored the user message: from then on the turn runs on the
    // server whatever happens to this connection.
    let userMessageConfirmed = false;
    // The stream dropped and the run is following the turn again.
    let reattaching = false;
    // The transcript before this send: a dropped send is checked against it
    // for a question the server stored after all.
    const knownMessageIDs = new Set(messages.map((message) => message.id));
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
        // Put the thread in the sidebar the moment it exists. Otherwise Recents
        // stays empty until the first thread event, and that event now arrives
        // only once the answer has finished — the title is generated from the
        // answer, so it cannot come any earlier. onThread below swaps the
        // generated title in; if no thread event ever arrives, the
        // receivedThreadEvent fallback upserts this same thread again.
        //
        // The interim label is the thread's stored title — the user's own
        // question, normalized server-side with its first letter capitalized —
        // rather than a "New thread" placeholder: while the answer streams that
        // question is the only thing telling this row apart from any other.
        //
        // The one prompt that leaves no title behind is one that normalizes away
        // to nothing (emoji-only, "###", a send that is pure attachments). The
        // server stores its English DefaultThreadTitle for those, so translate it
        // — a German UI has to read "Neuer Thread" here. This copy is what the
        // sidebar, the header and the end-of-turn fallback all read from, so the
        // label stays translated for the whole turn.
        const createdThread =
          targetThread.title === DEFAULT_THREAD_TITLE
            ? { ...targetThread, title: t("common.newThread") }
            : targetThread;
        createdThreadForFallback = createdThread;
        setThreads((current) => upsertThreadById(current, createdThread));
        if (
          projectIDForNewThread !== null &&
          createdThread.projectId === projectIDForNewThread
        ) {
          setProjectThreads((current) =>
            upsertThreadById(current, createdThread),
          );
        }
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
          // Any attachment the flush could not land stops the send: an image
          // without its artifact, a document without its document row, or one
          // that reported an error. Documents used to be dropped silently here,
          // so the message went out without the file the user attached.
          const failedAttachment = options.attachments.find(
            (attachment) =>
              attachment.status === "error" ||
              (isImageAttachment(attachment)
                ? attachment.artifactId === undefined
                : attachment.documentId === undefined),
          );
          if (failedAttachment !== undefined) {
            // The send stops here with the start screen still on show, so put the
            // files back rather than making the user pick them again.
            setPendingAttachments(attachmentsToFlush);
            throw new UserFacingError(
              failedAttachment.error ??
                t("errors.uploadFailed", {
                  filename: failedAttachment.filename,
                }),
            );
          }
        }
        // The run now belongs to a real thread. Rekeying in the same tick as the
        // route switch is what lets the thread we are about to land on pick the
        // turn up mid-flight.
        const createdRunKey = threadRunKey(targetThread.id);
        rekeyStreamRun(runKey, createdRunKey);
        runKey = createdRunKey;
        restoreScope = threadDraftScope(targetThread.id);
        // Creating the thread (and flushing uploads) took real time, during which
        // the start screen stayed interactive. Only land on the new thread if the
        // user is still where they sent from; if they went elsewhere, the turn
        // runs in the background and the thread waits in Recents.
        if (draftScopeKey(routeRef.current, false) === options.draftScope) {
          setActiveThread(createdThread);
          activeThreadIDRef.current = targetThread.id;
          setMessages([]);
          go({ view: "thread", threadID: targetThread.id });
        }
      }
      targetThreadID = targetThread.id;
      const threadIDForRun = targetThreadID;
      const makeTurn = () =>
        createTurnHandlers({
          patch: (next) => patchStreamRun(runKey, next),
          onUserMessage: (message) => {
            userMessageConfirmed = true;
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
          onAssistantMessage: (message, liveBlocks) => {
            if (isCurrentThread()) {
              setMessages((current) =>
                foldAssistantMessage(current, message, liveBlocks),
              );
            }
            // The settled message carries its own citations and blocks, so drop the
            // live copies now rather than at endRun — the stream reader yields
            // between chunks, so waiting would flash the turn twice.
          },
          onMessageCost: (event) => {
            if (isCurrentThread()) {
              setMessages((current) => applyMessageCost(current, event));
            }
          },
          onThread: (updatedThread) => {
            receivedThreadEvent = true;
            if (isCurrentThread()) setActiveThread(updatedThread);
            setThreads((current) => upsertThreadById(current, updatedThread));
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
        });
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
        const tempID = newTempID("temp-user");
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
      try {
        await streamMessage(
          threadIDForRun,
          content,
          makeTurn().handlers,
          abortController.signal,
          {
            documentAttachmentIds,
            imageAttachmentIds,
            pastedTexts: (options.pastedTexts ?? []).map(toPastedTextBlock),
          },
        );
      } catch (error) {
        // The connection dropped, typically a phone freezing the tab. Once the
        // server has the message the turn runs there, so follow it again
        // instead of failing the send. A drop before the server confirmed it
        // (fetch rejects with a TypeError) may still have stored it: ask.
        const dropped =
          error instanceof StreamInterruptedError ||
          (error instanceof TypeError && !userMessageConfirmed);
        if (
          !dropped ||
          abortController.signal.aborted ||
          stopRequested(abortController)
        )
          throw error;
        if (
          !userMessageConfirmed &&
          !(await sentMessageReachedServer(
            threadIDForRun,
            abortController.signal,
            content,
            knownMessageIDs,
          ))
        )
          throw error;
        reattaching = true;
        await resumeRunningTurn(
          threadIDForRun,
          abortController.signal,
          makeTurn,
          isCurrentThread,
        );
      }
      const fallbackThread = createdThreadForFallback;
      if (!receivedThreadEvent && fallbackThread !== null) {
        setThreads((current) => upsertThreadById(current, fallbackThread));
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
      // A stop the user asked for closes the stream server-side before the
      // client aborts its fetch, which reads as an interruption; it is not one.
      if (abortController.signal.aborted || stopRequested(abortController))
        return;
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
      // A failed reattach leaves the question on the server, where the turn
      // may still finish: restoring it would invite a duplicate send.
      if (options.restoreDraftOnError && !reattaching) {
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
      handleActionError(
        translateStreamError(error),
        t("thread.sendFailed"),
        (message) => {
          if (targetThreadID === null) reportShellError(message);
          else patchStreamRun(runKey, { error: message });
        },
      );
    } finally {
      endStreamRun(runKey, {
        keepFailedTurnVisible: keepFailedTurnVisible && targetThreadID !== null,
        controller: abortController,
      });
    }
  }

  // resumeRunningTurn follows a turn that is still running on the server. The
  // replay starts from the turn's first event, so each attempt gets fresh
  // handlers; their first flush replaces whatever streamed before the drop. A
  // turn that ended meanwhile is already saved: the thread is reloaded.
  async function resumeRunningTurn(
    threadID: string,
    signal: AbortSignal,
    makeTurn: () => ReturnType<typeof createTurnHandlers>,
    isCurrentThread: () => boolean,
  ) {
    const outcome = await followRunningTurn({
      threadId: threadID,
      signal,
      handlers: () => makeTurn().handlers,
    });
    if (outcome === "attached" || !isCurrentThread()) return;
    const response = await getThread(threadID);
    if (isCurrentThread())
      setMessages(response.messages.map(rehydrateLoadedMessage));
  }

  // sentMessageReachedServer reports whether the thread holds a new user
  // message with this content: a send whose connection dropped before the
  // server confirmed it may have been stored all the same.
  async function sentMessageReachedServer(
    threadID: string,
    signal: AbortSignal,
    content: string,
    knownMessageIDs: Set<string>,
  ): Promise<boolean> {
    await whenReachable(signal);
    try {
      const response = await getThread(threadID);
      return response.messages.some(
        (message) =>
          message.role === "user" &&
          !knownMessageIDs.has(message.id) &&
          message.content === content.trim(),
      );
    } catch {
      return false;
    }
  }

  // attachToRunningTurn picks up a turn found running when its thread loads: a
  // reload, or a phone that discarded the tab mid-answer. A run this tab already
  // has on the thread is left alone.
  attachToRunningTurnRef.current = (thread: Thread) => {
    const runKey = threadRunKey(thread.id);
    if (hasStreamRun(runKey)) return;
    const abortController = new AbortController();
    beginStreamRun(runKey, abortController);
    const isCurrentThread = () => activeThreadIDRef.current === thread.id;
    const makeTurn = () =>
      createTurnHandlers({
        patch: (next) => patchStreamRun(runKey, next),
        onUserMessage: (message) => {
          if (isCurrentThread())
            setMessages((current) =>
              reconcileUserMessage(current, null, message),
            );
        },
        onAssistantMessage: (message, liveBlocks) => {
          if (isCurrentThread())
            setMessages((current) =>
              foldAssistantMessage(current, message, liveBlocks),
            );
        },
        onMessageCost: (event) => {
          if (isCurrentThread())
            setMessages((current) => applyMessageCost(current, event));
        },
        onThread: (updatedThread) => {
          if (isCurrentThread()) setActiveThread(updatedThread);
          setThreads((current) => upsertThreadById(current, updatedThread));
          // A project page listing this thread shows the new title too.
          setProjectThreads((current) =>
            replaceThreadById(current, updatedThread),
          );
        },
      });
    let failed = false;
    resumeRunningTurn(
      thread.id,
      abortController.signal,
      makeTurn,
      isCurrentThread,
    )
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError")
          return;
        if (abortController.signal.aborted || stopRequested(abortController))
          return;
        failed = true;
        handleActionError(
          translateStreamError(error),
          t("thread.sendFailed"),
          (message) => patchStreamRun(runKey, { error: message }),
        );
      })
      .finally(() => {
        endStreamRun(runKey, {
          keepFailedTurnVisible: failed,
          controller: abortController,
        });
      });
  };

  async function handleIncognitoSend() {
    const draftText = draft.text.trim();
    const content = composeContent(draft);
    if (content === "") return;
    // Like handleSend: no send, and no slash command, while the turn streams.
    if (activeThreadIsStreaming) return;
    if (runSlashCommand(draftText)) return;
    await sendIncognitoContent(content, true, {
      draft: draftText,
      pastedTexts: draft.pastedTexts,
    });
  }

  // A failed turn's error belongs to its own thread; everything else (starring,
  // attaching, loading) belongs to the shell and shows wherever you are. The turn
  // error takes precedence only because reportShellError clears it first — so what
  // this really resolves to is whichever error happened most recently.
  const visibleSendError = activeRun.error !== "" ? activeRun.error : sendError;

  // The project list, for /projects and for a /projects/:id that names no
  // project we know; the two differ only in the error they show.
  function renderProjectsPage(projectsLoadError: string) {
    return (
      <Suspense fallback={null}>
        <ProjectsPage
          projects={projects}
          loadError={projectsLoadError}
          onOpenSidebar={openMobileSidebar}
          onCreateProject={() => openProjectDialog(null)}
          onOpenProject={navigateToProject}
          onEditProject={openProjectDialog}
          onArchiveProject={openArchiveProjectModal}
          onUnarchiveProject={unarchiveProjectAndReload}
          onDeleteProject={openDeleteProjectModal}
        />
      </Suspense>
    );
  }

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
      className={`relative grid h-svh grid-rows-[minmax(0,1fr)] bg-bg font-sans text-ink transition-[grid-template-columns] duration-200 ease-out grid-cols-[1fr] ${
        sidebarCollapsed
          ? "md:grid-cols-[56px_1fr]"
          : "md:grid-cols-[var(--ui-sidebar-w)_1fr]"
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
        onToggleDesktopCollapsed={toggleDesktopCollapsed}
        onCloseMobileSidebar={closeMobileSidebar}
        onToggleUserMenu={toggleUserMenu}
        onCloseUserMenu={closeUserMenu}
        onOpenSettings={openSettings}
        onLogout={onLogout}
        onAdmin={onAdmin}
        onNewThread={navigateToNew}
        onThreads={navigateToThreads}
        onArtifacts={navigateToArtifacts}
        onProjects={navigateToProjects}
        onMemory={navigateToMemory}
        onOpenSearch={openSearch}
        onSelectThread={selectThread}
        onDeleteThread={openDeleteModal}
        onRenameThread={openRenameModal}
        onAddThreadToProject={openMoveThreadModal}
        onStarThread={handleSetThreadStarred}
        onNavigateProject={navigateToProject}
        onStarProject={handleSetProjectStarred}
        onEditProject={openProjectDialog}
        onArchiveProject={openArchiveProjectModal}
        onDeleteProject={openDeleteProjectModal}
        onToggleThreadMenu={toggleThreadMenu}
        onCloseThreadMenu={closeThreadMenu}
      />
      {/* The sidebar's right edge, draggable from md up. Not while collapsed:
          the rail is a fixed 56px then, and there is nothing to size. */}
      {!sidebarCollapsed && <SidebarResizer />}
      <main className="min-h-0 min-w-0 overflow-hidden bg-bg">
        {showAdmin ? (
          adminPanel
        ) : route.view === "threads" ? (
          <ThreadsPage
            mutationVersion={threadMutationVersion}
            projectsAvailable={projects.length > 0}
            onOpenSidebar={openMobileSidebar}
            onNewThread={navigateToNew}
            onSelectThread={(threadID) => void selectThread(threadID)}
            onRenameThread={openRenameModal}
            onDeleteThread={openDeleteModal}
            onStarThread={(thread, starred, menuKey) =>
              void handleSetThreadStarred(thread, starred, menuKey)
            }
            onAddThreadToProject={openMoveThreadModal}
            onMoveSelectedToProject={openMoveModal}
            onAfterBulkDelete={reloadThreads}
            onSessionExpired={onSessionExpired}
          />
        ) : route.view === "artifacts" ? (
          <Suspense fallback={null}>
            <ArtifactsPage
              onOpenSidebar={openMobileSidebar}
              onSessionExpired={onSessionExpired}
              onUseInThread={handleUseArtifactInThread}
            />
          </Suspense>
        ) : route.view === "memory" ? (
          <Suspense fallback={null}>
            <MemoryPage onOpenSidebar={openMobileSidebar} />
          </Suspense>
        ) : route.view === "projects" ? (
          renderProjectsPage(loadError)
        ) : route.view === "project" ? (
          activeProject === null ? (
            renderProjectsPage(
              loadError === "" && threadDataLoaded
                ? t("errors.projectNotFound")
                : loadError,
            )
          ) : (
            <ProjectDetailPage
              project={activeProject}
              threads={projectThreads}
              draft={draft.text}
              sendError={visibleSendError}
              openThreadMenuID={openThreadMenuID}
              onBack={navigateToProjects}
              onSessionExpired={onSessionExpired}
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
              onToggleThreadMenu={toggleThreadMenu}
              onCloseThreadMenu={closeThreadMenu}
              onEditProject={openProjectDialog}
              onArchiveProject={openArchiveProjectModal}
              onUnarchiveProject={unarchiveProjectAndReload}
              onDeleteProject={openDeleteProjectModal}
              onToggleStar={(project, starred) =>
                void handleSetProjectStarred(project, starred)
              }
              onOpenSidebar={openMobileSidebar}
            />
          )
        ) : route.view === "new" ? (
          <StartPanel
            displayName={displayName}
            draft={draft.text}
            sendError={visibleSendError}
            attachments={pendingAttachments}
            attachNote={pendingAttachNote}
            onOpenSidebar={openMobileSidebar}
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
            onOpenSidebar={openMobileSidebar}
            messages={messages}
            draft={draft.text}
            streamingBlocks={activeRun.blocks}
            streamingSources={activeRun.sources}
            toolPending={activeRun.toolPending}
            workingTitle={activeRun.workingTitle}
            sendError={visibleSendError}
            isSending={activeThreadIsStreaming}
            openThreadMenuID={openThreadMenuID}
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
              projects.length === 0 ? undefined : openMoveThreadModal
            }
            onStarThread={(thread, starred, menuKey) =>
              void handleSetThreadStarred(thread, starred, menuKey)
            }
            onToggleThreadMenu={toggleThreadMenu}
            onCloseThreadMenu={closeThreadMenu}
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
      {settingsOpen && (
        <Suspense fallback={null}>
          <SettingsModal onClose={closeSettings} />
        </Suspense>
      )}
      {slashCommand !== null && (
        <SlashCommandPanel
          command={slashCommand}
          onClose={() => setSlashCommand(null)}
        />
      )}
      {searchOpen && (
        <SearchModal
          onClose={closeSearch}
          onSelectThread={(threadID) => void selectThread(threadID)}
          onSessionExpired={onSessionExpired}
        />
      )}
    </div>
  );
}
