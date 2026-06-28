import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import type { ActivityTraceEvent } from "../activityTrace";
import type { ContentBlock, Project, ShareInfo, Thread } from "../api";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { ThreadActionsMenu } from "../ThreadActionsMenu";
import { ShareDialog } from "../share/ShareDialog";
import { ActivityTracePanel } from "./ActivityTracePanel";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { GeneratedArtifactCard } from "./GeneratedArtifactCard";
import { Icon } from "./Icon";
import { AssistantProse, MessageBubble } from "./messages";
import { isImageAttachment, toSentAttachment, useDocumentAttachments, type ComposerAttachment } from "./useDocumentAttachments";
import { isNearBottom, previousUserContent } from "./threadUtils";
import type { MessageWithActivityTrace } from "./types";
import { WindowFileDrop } from "./WindowFileDrop";

export function ThreadPanel({
  thread,
  threadProject,
  share,
  onShareChange,
  deferredAttachNote,
  messages,
  draft,
  streamingBlocks,
  toolPending,
  sendError,
  isSending,
  sendDisabled,
  openThreadMenuID,
  onOpenSidebar,
  onDraftChange,
  onSend,
  onStop,
  onRetry,
  onOpenProject,
  onDeleteThread,
  onRenameThread,
  onAddToProject,
  onStarThread,
  onToggleThreadMenu,
  onCloseThreadMenu,
}: {
  thread: Thread | null;
  threadProject: Project | null;
  share: ShareInfo | null;
  onShareChange(next: ShareInfo | null): void;
  deferredAttachNote: string;
  messages: MessageWithActivityTrace[];
  draft: string;
  streamingBlocks: ContentBlock[];
  toolPending: boolean;
  sendError: string;
  isSending: boolean;
  sendDisabled: boolean;
  openThreadMenuID: string | null;
  onDraftChange(value: string): void;
  onSend(attachments?: ComposerAttachment[]): void;
  onStop(): void;
  onRetry(content: string): void;
  onOpenProject(project: Project): void;
  onDeleteThread(thread: Thread): void;
  onRenameThread(thread: Thread): void;
  onAddToProject?(thread: Thread): void;
  onStarThread(thread: Thread, starred: boolean, menuKey: string): void;
  onToggleThreadMenu(menuKey: string): void;
  onCloseThreadMenu(): void;
  onOpenSidebar(): void;
}) {
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const headerMenuRef = useRef<HTMLDivElement | null>(null);
  const shouldStickToBottomRef = useRef(true);
  const scrollFrameRef = useRef<number | null>(null);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);
  const [shareDialogOpen, setShareDialogOpen] = useState(false);
  // The thread has unshared content when its latest message is newer than the
  // share snapshot — this drives the dot badge and the "Update" affordance.
  const hasNewMessagesSinceShare =
    share?.shared === true &&
    thread?.lastMessageAt !== undefined &&
    new Date(thread.lastMessageAt).getTime() > new Date(share.snapshotAt).getTime();
  const headerMenuKey = thread === null ? null : `Header:${thread.id}`;
  const headerMenuOpen = headerMenuKey !== null && openThreadMenuID === headerMenuKey;
  // The live turn is an ordered block list (text / trace / artifact). The
  // currently-active activity panel is the LAST trace block; earlier completed
  // trace blocks render collapsed and inactive inline among the prose. The live
  // "Thinking" affordance and the auto-open latch derive from that active block's
  // events plus whether answer prose has begun streaming after it.
  const lastTraceBlockIndex = (() => {
    for (let index = streamingBlocks.length - 1; index >= 0; index -= 1) {
      if (streamingBlocks[index].type === "trace") return index;
    }
    return -1;
  })();
  const activeTraceEvents: ActivityTraceEvent[] =
    lastTraceBlockIndex === -1
      ? []
      : (streamingBlocks[lastTraceBlockIndex] as Extract<ContentBlock, { type: "trace" }>).events;
  const hasActiveActivityTrace = activeTraceEvents.length > 0;
  // Answer prose has begun once a non-empty text block exists after the active
  // trace block (the reasoning-free turn has no trace block, so any text block
  // counts). This is the block-model analog of the former `streamingText !== ""`.
  const answerTextStreaming = streamingBlocks.some(
    (block, index) => block.type === "text" && block.content !== "" && index > lastTraceBlockIndex,
  );
  // A live activity panel shows for the whole turn: either the active trace block,
  // or — when no trace has streamed yet — a standalone "Thinking" panel.
  const showStandaloneActiveTrace = lastTraceBlockIndex === -1 && isSending && sendError === "";
  const showActiveActivityTrace = hasActiveActivityTrace || showStandaloneActiveTrace;
  // The live thinking window manages its own open/closed state: it opens once per
  // turn when there is something to show and stays open through the answer, then
  // collapses when the turn ends. There is no persisted preference; past traces
  // simply start collapsed (uncontrolled).
  const [liveTraceExpanded, setLiveTraceExpanded] = useState(false);
  // Once the final answer streams (and no tool is running or pending), the
  // reasoning phase is over: show its abstract instead of "Thinking". Only
  // applies to traces that actually have reasoning, so reasoning-free turns keep
  // the "Thinking" affordance until they complete. `toolPending` bridges the gap
  // until a tool call the model has already started surfaces as a running event,
  // so streamed pre-tool preamble text never settles the label early. The label
  // also stays on "Thinking" until the latest reasoning round's background title
  // has arrived, so the raw-first-sentence fallback never flashes mid-stream.
  const hasReasoning = activeTraceEvents.some((event) => event.type === "reasoning");
  const toolRunning = activeTraceEvents.some((event) => event.type === "tool" && event.status === "running");
  const latestReasoning = activeTraceEvents.reduce<ActivityTraceEvent | undefined>(
    (acc, event) => (event.type === "reasoning" ? event : acc),
    undefined,
  );
  const latestReasoningTitled =
    latestReasoning?.type === "reasoning" && (latestReasoning.title?.trim() ?? "") !== "";
  // The answer phase has begun: reasoning produced content and prose is now
  // streaming, with no tool running or pending (the `toolPending` bridge keeps
  // pre-tool preamble text from triggering this early).
  const answerStarted = hasReasoning && answerTextStreaming && !toolRunning && !toolPending;
  // `reasoningSettled` is `answerStarted` plus the background reasoning title: it
  // gates the "Thinking" → abstract label flip, which must wait for the title so
  // the raw-first-sentence fallback never flashes mid-stream. The auto-collapse
  // below uses bare `answerStarted` instead, so the window collapses the instant
  // the answer starts rather than when the abstract happens to arrive.
  const reasoningSettled = answerStarted && latestReasoningTitled;
  // `liveTraceThinking` drives the "Thinking" affordance (active spinner), not the
  // open/closed state: the window stays open through the answer even after the
  // reasoning phase settles.
  const liveTraceThinking = isSending && !reasoningSettled;
  const liveTraceHasSomethingToShow = isSending && (hasActiveActivityTrace || answerTextStreaming);
  // Auto-open/collapse the live thinking window by tracking which phase the turn is
  // in: open while there is reasoning/tool activity to show and the answer has not
  // begun, collapse once the answer starts. This is applied only on the transition
  // (the desired state flipping), so a manual toggle in between sticks until the
  // next phase change — and a turn that resumes reasoning after an answer (e.g. a
  // second round around a tool call) re-opens, then re-collapses on the next
  // answer. The turn ending resets everything and collapses the now-past trace.
  const liveTraceAutoExpanded = liveTraceHasSomethingToShow && !answerStarted;
  const liveTraceAutoExpandedRef = useRef(false);
  useEffect(() => {
    if (!isSending) {
      liveTraceAutoExpandedRef.current = false;
      setLiveTraceExpanded(false);
      return;
    }
    if (liveTraceAutoExpanded !== liveTraceAutoExpandedRef.current) {
      liveTraceAutoExpandedRef.current = liveTraceAutoExpanded;
      setLiveTraceExpanded(liveTraceAutoExpanded);
    }
  }, [isSending, liveTraceAutoExpanded]);

  const refreshScrollState = useCallback(() => {
    const transcript = transcriptRef.current;
    if (transcript === null) return;
    // Only update the jump-to-bottom affordance here. Stick-to-bottom is never
    // re-engaged automatically: once the user scrolls away mid-stream it stays
    // disengaged until the next inference or an explicit jump-to-bottom.
    setShowJumpToBottom(!isNearBottom(transcript));
  }, []);

  const disengageAutoScroll = useCallback(() => {
    shouldStickToBottomRef.current = false;
    const transcript = transcriptRef.current;
    setShowJumpToBottom(transcript === null ? true : !isNearBottom(transcript));
  }, []);

  const handleWheel = useCallback(
    (event: React.WheelEvent<HTMLDivElement>) => {
      // A user wheel/trackpad gesture upward means they want to read back; stop
      // auto-scroll immediately so streaming content no longer yanks them down.
      if (event.deltaY < 0) disengageAutoScroll();
    },
    [disengageAutoScroll],
  );

  const scrollToLatest = useCallback(() => {
    const transcript = transcriptRef.current;
    if (transcript === null) return;
    const scroll = () => {
      transcript.scrollTop = transcript.scrollHeight;
    };
    scroll();
    if (scrollFrameRef.current !== null) window.cancelAnimationFrame(scrollFrameRef.current);
    scrollFrameRef.current = window.requestAnimationFrame(() => {
      scrollFrameRef.current = null;
      // Honour a user scroll that happened between the synchronous scroll above and this
      // frame: if they scrolled away, refreshScrollState cleared the flag, so don't yank
      // them back to the bottom.
      if (shouldStickToBottomRef.current) scroll();
    });
    shouldStickToBottomRef.current = true;
    setShowJumpToBottom(false);
  }, []);

  const pinToLatest = useCallback(() => {
    shouldStickToBottomRef.current = true;
    setShowJumpToBottom(false);
    scrollToLatest();
  }, [scrollToLatest]);

  const {
    attachNote,
    attachments,
    clearAttachments,
    handleAttachError,
    handleAttachFiles,
    removeAttachment,
  } = useDocumentAttachments({
    threadId: thread?.id,
    projectId: threadProject?.id,
  });

  const handleSendRequest = useCallback(() => {
    const sentAttachments = attachments.map(toSentAttachment);
    if (sentAttachments.length > 0) clearAttachments({ revokePreviewUrls: false });
    pinToLatest();
    onSend(sentAttachments);
  }, [attachments, clearAttachments, onSend, pinToLatest]);
  const imageUploadPending = attachments.some(
    (attachment) => isImageAttachment(attachment) && attachment.artifactId === undefined && attachment.status !== "error",
  );

  const handleRetryRequest = useCallback(
    (content: string) => {
      pinToLatest();
      onRetry(content);
    },
    [onRetry, pinToLatest],
  );

  useLayoutEffect(() => {
    shouldStickToBottomRef.current = true;
    setShowJumpToBottom(false);
    scrollToLatest();
  }, [scrollToLatest, thread?.id]);

  useLayoutEffect(() => {
    if (shouldStickToBottomRef.current) {
      scrollToLatest();
      return;
    }
    refreshScrollState();
  }, [
    messages.length,
    refreshScrollState,
    scrollToLatest,
    sendError,
    showActiveActivityTrace,
    streamingBlocks,
    liveTraceExpanded,
  ]);

  useEffect(() => {
    return () => {
      if (scrollFrameRef.current !== null) window.cancelAnimationFrame(scrollFrameRef.current);
    };
  }, []);

  useEffect(() => {
    if (!headerMenuOpen) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || headerMenuRef.current?.contains(target)) return;
      onCloseThreadMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [headerMenuOpen, onCloseThreadMenu]);

  return (
    <section className="flex h-svh min-h-0 flex-col">
      <header
        aria-label="Thread header"
        className="ui-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
        role="banner"
      >
        <div className="flex min-w-0 items-center gap-2">
          <SidebarOpenButton onClick={onOpenSidebar} />
          <div ref={headerMenuRef} className="relative flex min-w-0 items-center">
            <h1 className="flex min-w-0 max-w-[28ch] items-center gap-1 truncate font-sans font-normal sm:max-w-[48ch]">
              {threadProject !== null && thread !== null && (
                <>
                  <button
                    className="min-w-0 max-w-[12ch] truncate text-left text-[#d5d2c9] transition-colors hover:text-[#f3f0e8] sm:max-w-[22ch]"
                    onClick={() => onOpenProject(threadProject)}
                    type="button"
                  >
                    {threadProject.name}
                  </button>
                  <Icon name="chevronRight" size="16px" className="shrink-0 text-[#77736a]" />
                </>
              )}
              <span className="min-w-0 truncate">{thread?.title ?? "New thread"}</span>
            </h1>
            {thread !== null && headerMenuKey !== null && (
              <button
                aria-expanded={headerMenuOpen}
                aria-label="Open thread actions"
                className="ml-1 grid h-5 w-5 shrink-0 place-items-center rounded-md text-[#88857d] transition-colors hover:bg-[#2a2a28] hover:text-[#f3f0e8]"
                onClick={() => onToggleThreadMenu(headerMenuKey)}
                type="button"
              >
                <Icon name={headerMenuOpen ? "chevronDown" : "chevronRight"} size="16px" />
              </button>
            )}
            {thread !== null && headerMenuKey !== null && headerMenuOpen && (
              <ThreadActionsMenu
                menuKey={headerMenuKey}
                thread={thread}
                className="right-0 top-full"
                onDelete={onDeleteThread}
                onRename={onRenameThread}
                onShare={() => {
                  onCloseThreadMenu();
                  setShareDialogOpen(true);
                }}
                onAddToProject={onAddToProject}
                onStarChange={onStarThread}
              />
            )}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {thread !== null && (
            <button
              type="button"
              aria-label="Share chat"
              className="relative rounded-md px-2.5 py-0.5 text-[#d5d2c9] transition-colors hover:bg-[#2a2a28] hover:text-[#f3f0e8]"
              onClick={() => setShareDialogOpen(true)}
            >
              Share
              {hasNewMessagesSinceShare && (
                <span
                  aria-hidden
                  className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-[#c9a25f] ring-2 ring-[#1c1c19]"
                />
              )}
            </button>
          )}
        </div>
      </header>
      {thread !== null && shareDialogOpen && (
        <ShareDialog
          threadId={thread.id}
          share={share}
          hasNewMessages={hasNewMessagesSinceShare === true}
          onShareChange={onShareChange}
          onClose={() => setShareDialogOpen(false)}
        />
      )}
      <div className="relative min-h-0 flex-1">
        <div className="pointer-events-none absolute inset-x-0 top-0 z-10 h-8 bg-gradient-to-b from-bg to-transparent" />
        <div
          ref={transcriptRef}
          aria-label="Conversation transcript"
          className="flex h-full flex-col overflow-y-auto px-6 pt-10 [scrollbar-gutter:stable_both-edges] md:px-8"
          onScroll={refreshScrollState}
          onTouchMove={disengageAutoScroll}
          onWheel={handleWheel}
          role="region"
        >
          <div className="ui-thread-rail mx-auto w-full max-w-[720px] flex-1 space-y-6 pb-8">
            {messages.map((message, index) => (
              // Key off clientKey when present (an optimistic message keeps it across
              // reconcile to the persisted id) so the bubble's DOM node is stable.
              <div key={message.clientKey ?? message.id} className="space-y-6">
                <MessageBubble
                  message={message}
                  retryContent={message.role === "assistant" ? previousUserContent(messages, index) : null}
                  onRetry={handleRetryRequest}
                  category={thread?.category}
                />
              </div>
            ))}
            {/* The live streaming region is built as ONE keyed array so React's
                keyed reconciliation preserves DOM nodes across the turn. In
                particular the active trace panel keeps the stable key
                "live-active-trace" whether it is the pre-content placeholder (no
                trace block streamed yet) or the inline active trace block — so the
                first reasoning/tool event reuses the SAME node instead of
                unmounting the placeholder and mounting a fresh panel. React only
                preserves a keyed node across position changes within a single
                array, which is why both live in this one expression. */}
            {(() => {
              const elements: React.ReactNode[] = [];
              const hasTraceBlock = lastTraceBlockIndex !== -1;
              if (!hasTraceBlock && showStandaloneActiveTrace) {
                elements.push(
                  <ActivityTracePanel
                    key="live-active-trace"
                    events={[]}
                    active={liveTraceThinking}
                    streaming={isSending}
                    expanded={liveTraceExpanded}
                    onExpandedChange={setLiveTraceExpanded}
                  />,
                );
              }
              streamingBlocks.forEach((block, index) => {
                if (block.type === "text") {
                  if (block.content === "") return;
                  elements.push(
                    <AssistantProse key={`stream-text-${index}`} streaming>
                      {block.content}
                    </AssistantProse>,
                  );
                  return;
                }
                if (block.type === "artifact") {
                  elements.push(
                    <GeneratedArtifactCard key={`stream-artifact-${block.artifact.id}`} artifact={block.artifact} />,
                  );
                  return;
                }
                // Trace block: the active (last) one drives the live "Thinking"
                // affordance and stays expanded through the answer; earlier
                // completed trace blocks render collapsed and inactive. The active
                // one keeps the stable key shared with the placeholder above.
                const isActiveTrace = index === lastTraceBlockIndex;
                elements.push(
                  <ActivityTracePanel
                    key={isActiveTrace ? "live-active-trace" : `stream-trace-${index}`}
                    events={block.events}
                    active={isActiveTrace ? liveTraceThinking : false}
                    streaming={isActiveTrace ? isSending : false}
                    expanded={isActiveTrace ? liveTraceExpanded : undefined}
                    onExpandedChange={isActiveTrace ? setLiveTraceExpanded : undefined}
                  />,
                );
              });
              return elements;
            })()}
            {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
          </div>
          <div
            aria-label="Message composer dock"
            className="pointer-events-none sticky bottom-0 -mx-6 bg-bg px-6 pb-5 pt-4 md:-mx-8 md:px-8"
          >
            <div className="pointer-events-none absolute inset-x-0 bottom-full h-8 bg-gradient-to-t from-bg to-transparent" />
            <div className="ui-thread-rail pointer-events-auto mx-auto w-full max-w-[754px]">
              <WindowFileDrop
                enabled={thread !== null}
                onAttachFiles={handleAttachFiles}
                onAttachError={handleAttachError}
              />
              <Composer
                variant="thread"
                draft={draft}
                isSending={isSending}
                sendDisabled={sendDisabled || imageUploadPending}
                placeholder="Write a message..."
                onDraftChange={onDraftChange}
                onSend={handleSendRequest}
                onStop={onStop}
                onAttachFiles={thread === null ? undefined : handleAttachFiles}
                onAttachError={handleAttachError}
                attachments={attachments}
                onRemoveAttachment={removeAttachment}
              />
              {(attachNote || deferredAttachNote) !== "" && (
                <div className="ui-meta-text mt-2 text-center text-[#858178]">
                  {attachNote || deferredAttachNote}
                </div>
              )}
              <div className="ui-meta-text mt-2 text-center text-[#858178]">
                Loom can make mistakes. Please double-check responses.
              </div>
            </div>
          </div>
        </div>
        {showJumpToBottom && (
          <button
            aria-label="Jump to latest message"
            className="absolute bottom-40 left-1/2 grid h-9 w-9 -translate-x-1/2 place-items-center rounded-full border border-[#4b4a46] bg-[#2a2a28] text-[#f3f0e8] shadow-[0_10px_24px_rgba(0,0,0,0.35)] transition-colors hover:bg-[#343432]"
            onClick={scrollToLatest}
            title="Jump to latest"
            type="button"
          >
            <svg aria-hidden="true" className="h-4 w-4" fill="none" viewBox="0 0 24 24">
              <path
                d="M12 5v14M6.5 13.5 12 19l5.5-5.5"
                stroke="currentColor"
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2.2"
              />
            </svg>
          </button>
        )}
      </div>
    </section>
  );
}
