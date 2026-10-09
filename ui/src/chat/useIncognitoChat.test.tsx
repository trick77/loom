import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import type { Message, StreamHandlers } from "../api";
import i18n from "../i18n";
import { getDraft, INCOGNITO_DRAFT_SCOPE } from "./composerDrafts";
import { INCOGNITO_RUN_KEY } from "./streamRuns";
import { useComposerDrafts } from "./useComposerDrafts";
import { useIncognitoChat } from "./useIncognitoChat";
import { useStreamRuns } from "./useStreamRuns";

const api = vi.hoisted(() => ({
  streamIncognitoMessage: vi.fn(),
}));

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return { ...actual, ...api };
});

beforeEach(() => {
  api.streamIncognitoMessage.mockReset();
});

function assistantMessage(content: string): Message {
  return {
    id: "incognito-assistant",
    threadId: "incognito",
    role: "assistant",
    content,
    createdAt: "2026-10-09T00:00:00Z",
  };
}

function setup() {
  const runs = {
    begin: vi.fn(),
    patch: vi.fn(),
    end: vi.fn(),
    abort: vi.fn(),
  };
  const setSendError = vi.fn();
  const hook = renderHook(() => {
    const composer = useComposerDrafts();
    const chat = useIncognitoChat({
      runs: {},
      beginStreamRun: runs.begin,
      patchStreamRun: runs.patch,
      endStreamRun: runs.end,
      abortStreamRun: runs.abort,
      setDrafts: composer.setDrafts,
      requestComposerFocus: composer.requestFocus,
      setSendError,
      handleActionError: (_error, fallback, setError) => setError(fallback),
      translateStreamError: (error) => error,
    });
    return { composer, chat };
  });
  return { hook, runs, setSendError };
}

function incognitoDraft(hook: ReturnType<typeof setup>["hook"]) {
  return getDraft(hook.result.current.composer.drafts, INCOGNITO_DRAFT_SCOPE);
}

test("entering starts a clean incognito surface under its own run key", () => {
  const { hook, runs, setSendError } = setup();
  act(() => {
    hook.result.current.composer.setDraftText(INCOGNITO_DRAFT_SCOPE, "stale");
  });

  act(() => hook.result.current.chat.enterIncognito());

  expect(hook.result.current.chat.incognito).toBe(true);
  expect(hook.result.current.chat.incognitoMessages).toEqual([]);
  expect(incognitoDraft(hook).text).toBe("");
  expect(runs.abort).toHaveBeenCalledWith(INCOGNITO_RUN_KEY);
  expect(runs.end).toHaveBeenCalledWith(INCOGNITO_RUN_KEY, {
    keepFailedTurnVisible: false,
  });
  expect(setSendError).toHaveBeenCalledWith("");
});

test("exiting discards the transcript and the incognito draft", async () => {
  const { hook, runs } = setup();
  api.streamIncognitoMessage.mockImplementation(
    async (_content: string, _history: unknown, handlers: StreamHandlers) => {
      handlers.onAssistantMessage(assistantMessage("hi"));
    },
  );
  act(() => hook.result.current.chat.enterIncognito());
  await act(() => hook.result.current.chat.sendIncognitoContent("hello", true));
  act(() => {
    hook.result.current.composer.setDraftText(INCOGNITO_DRAFT_SCOPE, "unsent");
  });
  runs.abort.mockClear();

  act(() => hook.result.current.chat.exitIncognito());

  expect(hook.result.current.chat.incognito).toBe(false);
  expect(hook.result.current.chat.incognitoMessages).toEqual([]);
  expect(incognitoDraft(hook).text).toBe("");
  expect(runs.abort).toHaveBeenCalledWith(INCOGNITO_RUN_KEY);
});

test("a send replays the prior transcript and appends both turns", async () => {
  const { hook, runs } = setup();
  api.streamIncognitoMessage.mockImplementation(
    async (content: string, _history: unknown, handlers: StreamHandlers) => {
      handlers.onAssistantMessage(assistantMessage(`re: ${content}`));
    },
  );
  act(() => hook.result.current.chat.enterIncognito());

  await act(() => hook.result.current.chat.sendIncognitoContent("one", true));
  await act(() => hook.result.current.chat.sendIncognitoContent("two", true));

  expect(api.streamIncognitoMessage).toHaveBeenLastCalledWith(
    "two",
    [
      { role: "user", content: "one" },
      { role: "assistant", content: "re: one" },
    ],
    expect.anything(),
    expect.any(AbortSignal),
  );
  const messages = hook.result.current.chat.incognitoMessages;
  expect(messages.map((message) => [message.role, message.content])).toEqual([
    ["user", "one"],
    ["assistant", "re: one"],
    ["user", "two"],
    ["assistant", "re: two"],
  ]);
  // The server's constant synthetic id is replaced so React keys never collide.
  expect(messages[1]?.id).not.toBe(messages[3]?.id);
  expect(runs.begin).toHaveBeenCalledWith(
    INCOGNITO_RUN_KEY,
    expect.any(AbortController),
  );
  expect(runs.end).toHaveBeenLastCalledWith(INCOGNITO_RUN_KEY, {
    keepFailedTurnVisible: false,
    controller: expect.any(AbortController),
  });
});

test("a failed send drops the bubble, restores the draft and keeps the error", async () => {
  const { hook, runs } = setup();
  api.streamIncognitoMessage.mockRejectedValue(new Error("boom"));
  act(() => hook.result.current.chat.enterIncognito());
  const pasted = { id: "p1", text: "pasted body", lineCount: 1 };

  await act(() =>
    hook.result.current.chat.sendIncognitoContent(
      "draft\n\npasted body",
      true,
      {
        draft: "draft",
        pastedTexts: [pasted],
      },
    ),
  );

  expect(hook.result.current.chat.incognitoMessages).toEqual([]);
  expect(incognitoDraft(hook)).toEqual({
    text: "draft",
    pastedTexts: [pasted],
  });
  expect(runs.patch).toHaveBeenCalledWith(INCOGNITO_RUN_KEY, {
    error: i18n.t("thread.sendFailed"),
  });
  expect(runs.end).toHaveBeenLastCalledWith(INCOGNITO_RUN_KEY, {
    keepFailedTurnVisible: true,
    controller: expect.any(AbortController),
  });
});

test("a send while the incognito turn is streaming is ignored", async () => {
  let finishFirst = () => {};
  api.streamIncognitoMessage.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finishFirst = resolve;
      }),
  );
  const { result } = renderHook(() => {
    const composer = useComposerDrafts();
    const runs = useStreamRuns();
    const chat = useIncognitoChat({
      runs: runs.runs,
      beginStreamRun: runs.begin,
      patchStreamRun: runs.patch,
      endStreamRun: runs.end,
      abortStreamRun: runs.abort,
      setDrafts: composer.setDrafts,
      requestComposerFocus: composer.requestFocus,
      setSendError: () => {},
      handleActionError: (_error, fallback, setError) => setError(fallback),
      translateStreamError: (error) => error,
    });
    return { chat };
  });

  let first: Promise<void> = Promise.resolve();
  act(() => {
    first = result.current.chat.sendIncognitoContent("one", true);
  });
  await act(() => result.current.chat.sendIncognitoContent("two", true));

  expect(api.streamIncognitoMessage).toHaveBeenCalledTimes(1);
  expect(
    result.current.chat.incognitoMessages.map((message) => message.content),
  ).toEqual(["one"]);
  await act(async () => {
    finishFirst();
    await first;
  });
});

test("two sends in the same render start only one turn", async () => {
  let finishFirst = () => {};
  api.streamIncognitoMessage.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finishFirst = resolve;
      }),
  );
  const { hook } = setup();

  let sends: Promise<void>[] = [];
  act(() => {
    sends = [
      hook.result.current.chat.sendIncognitoContent("one", true),
      hook.result.current.chat.sendIncognitoContent("two", true),
    ];
  });

  expect(api.streamIncognitoMessage).toHaveBeenCalledTimes(1);
  await act(async () => {
    finishFirst();
    await Promise.all(sends);
  });
});

test("an aborted send ends quietly", async () => {
  const { hook, runs } = setup();
  api.streamIncognitoMessage.mockRejectedValue(
    new DOMException("aborted", "AbortError"),
  );
  act(() => hook.result.current.chat.enterIncognito());

  await act(() => hook.result.current.chat.sendIncognitoContent("hello", true));

  expect(runs.patch).not.toHaveBeenCalledWith(
    INCOGNITO_RUN_KEY,
    expect.objectContaining({ error: expect.anything() }),
  );
  expect(runs.end).toHaveBeenLastCalledWith(INCOGNITO_RUN_KEY, {
    keepFailedTurnVisible: false,
    controller: expect.any(AbortController),
  });
});

test("retry loads the message back into the incognito composer", () => {
  const { hook } = setup();
  const focusBefore = hook.result.current.composer.focusTick;

  act(() =>
    hook.result.current.chat.handleIncognitoRetry("again", [
      { text: "block", lineCount: 1 },
    ]),
  );

  expect(incognitoDraft(hook).text).toBe("again");
  expect(incognitoDraft(hook).pastedTexts).toHaveLength(1);
  expect(hook.result.current.composer.focusTick).toBe(focusBefore + 1);
});

test("retry ignores an empty message", () => {
  const { hook } = setup();
  const focusBefore = hook.result.current.composer.focusTick;

  act(() => hook.result.current.chat.handleIncognitoRetry("  "));

  expect(incognitoDraft(hook).text).toBe("");
  expect(hook.result.current.composer.focusTick).toBe(focusBefore);
});
