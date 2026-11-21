import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import type { ActionCardData } from "../api";
import { ActionCard, actionRows, forgetActionOutcomes } from "./ActionCard";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    runToolAction: vi.fn(),
    dismissToolAction: vi.fn(),
    getToolAction: vi.fn(),
  };
});

const card = (over: Partial<ActionCardData> = {}): ActionCardData => ({
  actionId: "a1",
  state: "proposed",
  server: "peeq",
  title: "Add videos to your library",
  arguments: {
    videos: [
      {
        url: "https://www.youtube.com/watch?v=Qm4tR8xLp2A",
        title: "Stage 17 Highlights",
      },
      { url: "https://youtu.be/Zr7KcW1nB0e", title: "Aero socks" },
      { url: "javascript:alert(1)", title: "**Bold** <img src=x>" },
    ],
  },
  ...over,
});

beforeEach(() => {
  forgetActionOutcomes();
  vi.clearAllMocks();
});

// Nothing runs until the click; the card shows what the click sends, as
// plain text, with links only for http(s).
test("a proposal shows what it sends and runs on the click", async () => {
  vi.mocked(api.runToolAction).mockResolvedValue(
    card({
      state: "done",
      result: {
        text: "queued",
        items: [
          { status: "done", message: "queued" },
          { status: "skipped", message: "already in library" },
          { status: "failed", message: "Paste a single video link" },
        ],
        link: { url: "https://peeq.example/activity", label: "peeq" },
      },
    }),
  );
  const { container } = render(<ActionCard card={card()} />);
  expect(screen.getByText("Add videos to your library")).toBeInTheDocument();
  expect(screen.getByText("peeq · run?")).toBeInTheDocument();
  expect(screen.getByText("**Bold** <img src=x>")).toBeInTheDocument();
  expect(container.querySelector("img, strong")).toBeNull();
  const links = [...container.querySelectorAll("a")].map((a) =>
    a.getAttribute("href"),
  );
  expect(links).toEqual([
    "https://www.youtube.com/watch?v=Qm4tR8xLp2A",
    "https://youtu.be/Zr7KcW1nB0e",
  ]);
  expect(screen.getByText("youtube.com/watch?v=Qm4tR8xLp2A")).toBeVisible();
  expect(api.runToolAction).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "Run" }));
  await waitFor(() =>
    expect(
      screen.getByText("peeq · 1 done · 1 skipped · 1 failed"),
    ).toBeInTheDocument(),
  );
  expect(api.runToolAction).toHaveBeenCalledWith("a1");
  expect(screen.getByText("queued")).toBeInTheDocument();
  expect(screen.getByText("already in library")).toBeInTheDocument();
  expect(screen.getByText("Paste a single video link")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: /Open peeq/ })).toHaveAttribute(
    "href",
    "https://peeq.example/activity",
  );
  expect(screen.queryByRole("button", { name: "Run" })).toBeNull();
});

test("dismiss turns the card into one quiet line", async () => {
  vi.mocked(api.dismissToolAction).mockResolvedValue(
    card({ state: "dismissed" }),
  );
  render(<ActionCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
  await waitFor(() => expect(screen.getByText("Dismissed")).toBeVisible());
  expect(api.runToolAction).not.toHaveBeenCalled();
});

test("a card the server no longer has is gone", async () => {
  vi.mocked(api.runToolAction).mockRejectedValue(new api.ToolActionGoneError());
  render(<ActionCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Run" }));
  await waitFor(() =>
    expect(screen.getByText("No longer available")).toBeVisible(),
  );
});

test("a network failure keeps the card asking and says so", async () => {
  vi.mocked(api.runToolAction).mockRejectedValue(new Error("boom"));
  render(<ActionCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Run" }));
  await waitFor(() =>
    expect(
      screen.getByText("Couldn't reach the server. Try again."),
    ).toBeVisible(),
  );
  expect(screen.getByRole("button", { name: "Run" })).toBeEnabled();
});

test("a failed call shows the server's reason, or a generic line", () => {
  const { rerender } = render(
    <ActionCard
      card={card({
        state: "failed",
        result: { text: "at most 10 videos per call" },
      })}
    />,
  );
  expect(screen.getByText("at most 10 videos per call")).toBeVisible();
  expect(screen.getByText("peeq · failed")).toBeVisible();
  rerender(<ActionCard card={card({ actionId: "a2", state: "failed" })} />);
  expect(
    screen.getByText("The call failed. Ask again in the chat to retry."),
  ).toBeVisible();
});

test("a running card waits with its buttons off", () => {
  render(<ActionCard card={card({ state: "running" })} />);
  expect(screen.getByRole("button", { name: "Running…" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Dismiss" })).toBeDisabled();
});

// Every field the click sends is on the card: a field the title and link do
// not show comes as "name: value", a url that is no http(s) link as text.
test("fields beside the title and link are shown too", () => {
  const { container } = render(
    <ActionCard
      card={card({
        arguments: {
          videos: [
            {
              url: "https://youtu.be/abc",
              title: "Aero socks",
              collection: "public",
            },
            { url: "file:///etc/passwd", title: "Local" },
          ],
        },
      })}
    />,
  );
  expect(screen.getByText("collection: public")).toBeVisible();
  expect(screen.getByText("url: file:///etc/passwd")).toBeVisible();
  expect(container.querySelectorAll("a")).toHaveLength(1);
});

test("a status loom does not know is never shown as done", () => {
  render(
    <ActionCard
      card={card({
        state: "done",
        arguments: { videos: ["https://youtu.be/abc"] },
        result: { text: "", items: [{ status: "", message: "" }] },
      })}
    />,
  );
  expect(screen.getByText("unknown")).toBeVisible();
  expect(screen.getByText("peeq · done")).toBeVisible();
});

test("a call that never settled says so, with no retry advice", () => {
  render(<ActionCard card={card({ state: "unknown" })} />);
  expect(screen.getByText("peeq · outcome unknown")).toBeVisible();
  expect(
    screen.getByText(/Check in peeq whether it went through/),
  ).toBeVisible();
  expect(screen.queryByRole("button", { name: "Run" })).toBeNull();
});

test("a server error does not claim the server is unreachable", async () => {
  vi.mocked(api.runToolAction).mockRejectedValue(
    new api.ToolActionServerError(500),
  );
  render(<ActionCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Run" }));
  await waitFor(() =>
    expect(screen.getByText(/Reload the thread/)).toBeVisible(),
  );
});

// A call running from another tab settles there: the card asks until it has.
test("a card running elsewhere polls until it settles", async () => {
  vi.useFakeTimers();
  try {
    vi.mocked(api.getToolAction)
      .mockResolvedValueOnce(card({ state: "running" }))
      .mockResolvedValueOnce(card({ state: "done", result: { text: "ok" } }));
    render(<ActionCard card={card({ state: "running" })} />);
    await vi.advanceTimersByTimeAsync(3000);
    expect(screen.getByRole("button", { name: "Running…" })).toBeDisabled();
    await vi.advanceTimersByTimeAsync(3000);
    expect(screen.getByText("peeq · done")).toBeInTheDocument();
    expect(api.getToolAction).toHaveBeenCalledTimes(2);
  } finally {
    vi.useRealTimers();
  }
});

test("arguments lay out as the first array's rows, then the rest", () => {
  const { list, rest } = actionRows({
    note: "hi",
    urls: ["https://a.example/x", "plain"],
    more: [1],
    count: 3,
  });
  expect(list).toEqual([
    { title: "", url: "https://a.example/x", extra: [] },
    { title: "plain", url: null, extra: [] },
  ]);
  expect(rest.map((row) => row.title)).toEqual([
    "note: hi",
    "more: [1]",
    "count: 3",
  ]);
});
