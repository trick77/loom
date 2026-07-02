import { useEffect, useLayoutEffect, useRef } from "react";

import type { ContentBlock } from "../api";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { Icon } from "./Icon";
import { AssistantProse, MessageBubble } from "./messages";
import { ActivityTracePanel } from "./ActivityTracePanel";
import type { MessageWithActivityTrace } from "./types";
import { previousUserContent } from "./threadUtils";
import { WorkingDot } from "./WorkingDot";
import loomLogo from "../assets/loom-logo.svg";

// IncognitoPanel is the standalone ephemeral-chat view. It never touches the
// sidebar, thread lists, or persistence — the transcript lives entirely in the
// caller's in-memory state and is discarded on exit. Because incognito runs
// tool-free, the only live blocks are text and reasoning traces, so the streaming
// render is a trimmed version of ThreadPanel's (no artifacts / tool panels).
export function IncognitoPanel({
  messages,
  draft,
  streamingBlocks,
  isSending,
  sendError,
  onDraftChange,
  onSend,
  onStop,
  onRetry,
  onExit,
}: {
  messages: MessageWithActivityTrace[];
  draft: string;
  streamingBlocks: ContentBlock[];
  isSending: boolean;
  sendError: string;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
  onRetry(content: string): void;
  onExit(): void;
}) {
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const isEmpty = messages.length === 0 && !isSending;

  // Keep the transcript pinned to the latest content as it streams. Incognito has
  // no read-back affordance to preserve, so a plain stick-to-bottom is enough.
  useLayoutEffect(() => {
    const el = transcriptRef.current;
    if (el !== null) el.scrollTop = el.scrollHeight;
  });

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && !isSending) onExit();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isSending, onExit]);

  const notice = (
    <div className="ui-meta-text mt-3 text-center text-[#5599e7]">
      Incognito threads aren't saved, added to memory, or used to train models.
    </div>
  );

  const composer = (
    <Composer
      variant={isEmpty ? "start" : "thread"}
      incognito
      autoFocus
      draft={draft}
      isSending={isSending}
      placeholder="Message incognito..."
      onDraftChange={onDraftChange}
      onSend={onSend}
      onStop={onStop}
    />
  );

  const streamingElements: React.ReactNode[] = [];
  let answerStreaming = false;
  streamingBlocks.forEach((block, index) => {
    if (block.type === "text") {
      if (block.content === "") return;
      answerStreaming = true;
      streamingElements.push(
        <AssistantProse key={`stream-text-${index}`} streaming>
          {block.content}
        </AssistantProse>,
      );
      return;
    }
    if (block.type === "trace") {
      streamingElements.push(
        <ActivityTracePanel
          key={`stream-trace-${index}`}
          events={block.events}
          active={isSending}
          streaming={isSending}
        />,
      );
    }
    // Incognito is tool-free, so artifact blocks never occur here.
  });
  const working = isSending && sendError === "" && !answerStreaming;

  return (
    <section className="flex h-svh min-h-0 flex-col bg-bg p-2 sm:p-3">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-[20px] border border-dashed border-[#4b4a46] bg-[#141412]">
        <header
          aria-label="Incognito thread header"
          className="ui-control-text flex h-11 shrink-0 items-center justify-between gap-3 px-4 text-[#d5d2c9]"
        >
          <div className="flex min-w-0 items-center gap-2">
            <Icon name="ghost" size="18px" />
            <span className="font-sans">Incognito thread</span>
          </div>
          <button
            aria-label="Exit incognito"
            className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] transition-colors hover:bg-[#2a2a28] hover:text-[#f3f0e8]"
            onClick={onExit}
            type="button"
          >
            <Icon name="close" size="18px" />
          </button>
        </header>

        {isEmpty ? (
          <div className="flex min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-4 pb-[8vh] sm:px-8">
            <h2 className="ui-greeting-text mb-8 flex items-center gap-2.5 font-serif">
              <img src={loomLogo} alt="" aria-hidden className="h-10 w-10 -translate-y-1" />
              <span className="-translate-y-0.5">Let's chat incognito</span>
            </h2>
            <div className="w-full max-w-[674px]">
              {composer}
              {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
              {notice}
            </div>
          </div>
        ) : (
          <div
            ref={transcriptRef}
            aria-label="Incognito conversation transcript"
            className="flex min-h-0 flex-1 flex-col overflow-y-auto px-6 pt-6 md:px-8"
            role="region"
          >
            <div className="mx-auto w-full max-w-[720px] flex-1 space-y-6 pb-8">
              {messages.map((message, index) => (
                <div key={message.clientKey ?? message.id} className="space-y-6">
                  <MessageBubble
                    message={message}
                    retryContent={
                      message.role === "assistant" ? previousUserContent(messages, index) : null
                    }
                    onRetry={onRetry}
                  />
                </div>
              ))}
              {streamingElements}
              {working && <WorkingDot />}
              {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
            </div>
            <div
              aria-label="Message composer dock"
              className="sticky bottom-0 -mx-6 bg-[#141412] px-6 pb-4 pt-3 md:-mx-8 md:px-8"
            >
              <div className="mx-auto w-full max-w-[754px]">
                {composer}
                {notice}
              </div>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
