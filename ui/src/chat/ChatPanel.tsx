import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import type { ActivityTraceEvent } from "../activityTrace";
import type { Artifact, McpStatusEvent, Project, Thread } from "../api";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { ThreadActionsMenu } from "../ThreadActionsMenu";
import { ActivityTracePanel } from "./ActivityTracePanel";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { pendingArtifactLabels } from "./artifacts";
import { GeneratedArtifactCard } from "./GeneratedArtifactCard";
import { Icon } from "./Icon";
import { AssistantText, MessageBubble, PendingArtifactCard } from "./messages";
import { isImageAttachment, toSentAttachment, useDocumentAttachments, type ComposerAttachment } from "./useDocumentAttachments";
import { isNearBottom, previousUserContent } from "./chatUtils";
import type { MessageWithActivityTrace } from "./types";
import { McpStatusIndicator } from "./SidebarItems";
import { WindowFileDrop } from "./WindowFileDrop";

export function ChatPanel({
  thread,
  threadProject,
  deferredAttachNote,
  messages,
  draft,
  streamingText,
  streamingArtifacts,
  activityTrace,
  toolPending,
  sendError,
  isSending,
  sendDisabled,
  mcpStatus,
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
  deferredAttachNote: string;
  messages: MessageWithActivityTrace[];
  draft: string;
  streamingText: string;
  streamingArtifacts: Artifact[];
  activityTrace: ActivityTraceEvent[];
  toolPending: boolean;
  sendError: string;
  isSending: boolean;
  sendDisabled: boolean;
  mcpStatus: McpStatusEvent | null;
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
  const headerMenuKey = thread === null ? null : `Header:${thread.id}`;
  const headerMenuOpen = headerMenuKey !== null && openThreadMenuID === headerMenuKey;
  const hasActiveActivityTrace = activityTrace.length > 0;
  const showActiveActivityTrace = hasActiveActivityTrace || (isSending && sendError === "");
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
  const hasReasoning = activityTrace.some((event) => event.type === "reasoning");
  const toolRunning = activityTrace.some((event) => event.type === "tool" && event.status === "running");
  const latestReasoning = activityTrace.reduce<ActivityTraceEvent | undefined>(
    (acc, event) => (event.type === "reasoning" ? event : acc),
    undefined,
  );
  const latestReasoningTitled =
    latestReasoning?.type === "reasoning" && (latestReasoning.title?.trim() ?? "") !== "";
  const reasoningSettled = hasReasoning && streamingText !== "" && !toolRunning && !toolPending && latestReasoningTitled;
  // `liveTraceThinking` drives the "Thinking" affordance (active spinner), not the
  // open/closed state: the window stays open through the answer even after the
  // reasoning phase settles.
  const liveTraceThinking = isSending && !reasoningSettled;
  // Auto-open the live thinking window exactly once per turn. Stay collapsed at the
  // bare start of a turn (nothing to show yet); open the first moment there is
  // something to show — reasoning/tool content has arrived, or the answer has begun
  // streaming (the reasoning-free case). Once opened it stays open for the rest of
  // the turn, so a manual collapse in between sticks. The turn ending resets the
  // latch and collapses the now-past trace.
  const liveTraceHasSomethingToShow = isSending && (hasActiveActivityTrace || streamingText !== "");
  const liveTraceAutoOpenedRef = useRef(false);
  useEffect(() => {
    if (!isSending) {
      liveTraceAutoOpenedRef.current = false;
      setLiveTraceExpanded(false);
      return;
    }
    if (liveTraceHasSomethingToShow && !liveTraceAutoOpenedRef.current) {
      liveTraceAutoOpenedRef.current = true;
      setLiveTraceExpanded(true);
    }
  }, [isSending, liveTraceHasSomethingToShow]);

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
    streamingArtifacts.length,
    streamingText,
    activityTrace,
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
        aria-label="Chat header"
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
              <span className="min-w-0 truncate">{thread?.title ?? "New chat"}</span>
            </h1>
            {thread !== null && headerMenuKey !== null && (
              <button
                aria-expanded={headerMenuOpen}
                aria-label="Open chat actions"
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
                onAddToProject={onAddToProject}
                onStarChange={onStarThread}
              />
            )}
          </div>
        </div>
        {mcpStatus !== null && mcpStatus.configured > 0 && <McpStatusIndicator compact status={mcpStatus} />}
      </header>
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
          <div className="ui-chat-rail mx-auto w-full max-w-[720px] flex-1 space-y-6 pb-8">
            {messages.map((message, index) => (
              <div key={message.id} className="space-y-6">
                {message.role === "assistant" && message.activityTrace !== undefined && (
                  <ActivityTracePanel events={message.activityTrace} active={false} />
                )}
                {message.role === "assistant" && message.activityTrace === undefined && message.reasoningContent && (
                  <ActivityTracePanel
                    events={[
                      {
                        id: `${message.id}-reasoning`,
                        type: "reasoning",
                        content: message.reasoningContent,
                        status: "done",
                      },
                    ]}
                    active={false}
                  />
                )}
                <MessageBubble
                  message={message}
                  retryContent={message.role === "assistant" ? previousUserContent(messages, index) : null}
                  onRetry={handleRetryRequest}
                />
              </div>
            ))}
            {showActiveActivityTrace && (
              <ActivityTracePanel
                events={activityTrace}
                active={liveTraceThinking}
                streaming={isSending}
                expanded={liveTraceExpanded}
                onExpandedChange={setLiveTraceExpanded}
              />
            )}
            {streamingText !== "" && <AssistantText streaming>{streamingText}</AssistantText>}
            {streamingArtifacts.map((artifact) => (
              <GeneratedArtifactCard key={artifact.id} artifact={artifact} />
            ))}
            {pendingArtifactLabels(activityTrace, streamingArtifacts.length).map((label, index) => (
              <PendingArtifactCard key={`pending-artifact-${index}`} label={label} />
            ))}
            {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
          </div>
          <div
            aria-label="Message composer dock"
            className="pointer-events-none sticky bottom-0 -mx-6 bg-bg px-6 pb-5 pt-4 md:-mx-8 md:px-8"
          >
            <div className="pointer-events-none absolute inset-x-0 bottom-full h-8 bg-gradient-to-t from-bg to-transparent" />
            <div className="ui-chat-rail pointer-events-auto mx-auto w-full max-w-[754px]">
              <WindowFileDrop
                enabled={thread !== null}
                onAttachFiles={handleAttachFiles}
                onAttachError={handleAttachError}
              />
              <Composer
                variant="chat"
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
                Slopr can make mistakes. Please double-check responses.
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
