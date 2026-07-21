import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { IncognitoPanel } from "./IncognitoPanel";
import type { MessageWithActivityTrace } from "./types";

const userMessage: MessageWithActivityTrace = {
  id: "m1",
  threadId: "incognito",
  role: "user",
  content: "Compare the gateways",
  createdAt: "2026-06-28T00:00:00Z",
};

const assistantMessage: MessageWithActivityTrace = {
  id: "m2",
  threadId: "incognito",
  role: "assistant",
  content: "Here is the answer",
  contentBlocks: [{ type: "text", content: "Here is the answer" }],
  createdAt: "2026-06-28T00:00:01Z",
};

function renderPanel(
  overrides: Partial<Parameters<typeof IncognitoPanel>[0]> = {},
) {
  const props = {
    messages: [] as MessageWithActivityTrace[],
    draft: "",
    streamingBlocks: [],
    isSending: false,
    sendError: "",
    reasoningEffort: "high" as const,
    onReasoningEffortChange: vi.fn(),
    onDraftChange: vi.fn(),
    pastedTexts: [],
    onAddPastedText: vi.fn(),
    onRemovePastedText: vi.fn(),
    onSend: vi.fn(),
    onStop: vi.fn(),
    onRetry: vi.fn(),
    onExit: vi.fn(),
    ...overrides,
  };
  render(<IncognitoPanel {...props} />);
  return props;
}

describe("IncognitoPanel", () => {
  it("renders the empty start state with the greeting and no transcript", () => {
    renderPanel();

    expect(screen.getByText("Let's chat incognito")).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText("Message incognito..."),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Incognito threads aren't saved, added to memory, or used to train models.",
      ),
    ).toBeInTheDocument();
    // The transcript region only exists once there is something to show.
    expect(
      screen.queryByRole("region", {
        name: "Incognito conversation transcript",
      }),
    ).not.toBeInTheDocument();
  });

  it("switches to the transcript view once messages exist", () => {
    renderPanel({ messages: [userMessage, assistantMessage] });

    expect(screen.queryByText("Let's chat incognito")).not.toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "Incognito conversation transcript" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Compare the gateways")).toBeInTheDocument();
    expect(screen.getByText("Here is the answer")).toBeInTheDocument();
  });

  it("leaves the start state while a first response is still sending", () => {
    renderPanel({ isSending: true });

    expect(screen.queryByText("Let's chat incognito")).not.toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "Incognito conversation transcript" }),
    ).toBeInTheDocument();
  });

  it("renders streaming text blocks and skips empty ones", () => {
    renderPanel({
      messages: [userMessage],
      isSending: true,
      streamingBlocks: [
        { type: "text", content: "" },
        { type: "text", content: "Partial answer" },
      ],
    });

    expect(screen.getByText("Partial answer")).toBeInTheDocument();
  });

  it("renders a streaming trace block as an activity panel", () => {
    renderPanel({
      messages: [userMessage],
      isSending: true,
      streamingBlocks: [
        {
          type: "trace",
          events: [
            {
              id: "call_search",
              type: "tool",
              name: "search_web",
              status: "running",
              summary: { kind: "generated", title: "Searching the web" },
            },
          ],
        },
      ],
    });

    expect(
      screen.getByRole("status", { name: /loom activity trace/i }),
    ).toBeInTheDocument();
    // The panel starts collapsed; expanding it reveals the streamed event.
    fireEvent.click(screen.getByRole("button", { name: "Show activity" }));
    expect(screen.getByText("Searching the web")).toBeInTheDocument();
  });

  it("shows the working dot only while sending with no answer text yet", () => {
    const { rerender } = render(
      <IncognitoPanel
        messages={[userMessage]}
        draft=""
        streamingBlocks={[]}
        isSending
        sendError=""
        reasoningEffort="high"
        onReasoningEffortChange={vi.fn()}
        onDraftChange={vi.fn()}
        pastedTexts={[]}
        onAddPastedText={vi.fn()}
        onRemovePastedText={vi.fn()}
        onSend={vi.fn()}
        onStop={vi.fn()}
        onRetry={vi.fn()}
        onExit={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toBeInTheDocument();

    // Once prose starts streaming the dot gives way to the answer.
    rerender(
      <IncognitoPanel
        messages={[userMessage]}
        draft=""
        streamingBlocks={[{ type: "text", content: "Answering" }]}
        isSending
        sendError=""
        reasoningEffort="high"
        onReasoningEffortChange={vi.fn()}
        onDraftChange={vi.fn()}
        pastedTexts={[]}
        onAddPastedText={vi.fn()}
        onRemovePastedText={vi.fn()}
        onSend={vi.fn()}
        onStop={vi.fn()}
        onRetry={vi.fn()}
        onExit={vi.fn()}
      />,
    );
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByText("Answering")).toBeInTheDocument();
  });

  it("shows the send error in both the start and transcript layouts", () => {
    const { unmount } = render(
      <IncognitoPanel
        messages={[]}
        draft=""
        streamingBlocks={[]}
        isSending={false}
        sendError="Model unavailable"
        reasoningEffort="high"
        onReasoningEffortChange={vi.fn()}
        onDraftChange={vi.fn()}
        pastedTexts={[]}
        onAddPastedText={vi.fn()}
        onRemovePastedText={vi.fn()}
        onSend={vi.fn()}
        onStop={vi.fn()}
        onRetry={vi.fn()}
        onExit={vi.fn()}
      />,
    );
    expect(screen.getByText("Model unavailable")).toBeInTheDocument();
    unmount();

    renderPanel({ messages: [userMessage], sendError: "Model unavailable" });
    expect(screen.getByText("Model unavailable")).toBeInTheDocument();
  });

  it("exits via the header button", () => {
    const { onExit } = renderPanel();

    fireEvent.click(screen.getByRole("button", { name: "Exit incognito" }));

    expect(onExit).toHaveBeenCalledOnce();
  });

  it("exits on Escape when idle", () => {
    const { onExit } = renderPanel();

    fireEvent.keyDown(window, { key: "Escape" });

    expect(onExit).toHaveBeenCalledOnce();
  });

  it("ignores Escape while a response is in flight", () => {
    const { onExit } = renderPanel({
      isSending: true,
      messages: [userMessage],
    });

    fireEvent.keyDown(window, { key: "Escape" });

    expect(onExit).not.toHaveBeenCalled();
  });

  it("forwards the draft the user types to the parent", () => {
    const { onDraftChange } = renderPanel();

    fireEvent.change(screen.getByPlaceholderText("Message incognito..."), {
      target: { value: "hello" },
    });

    expect(onDraftChange).toHaveBeenCalledWith("hello");
  });
});
