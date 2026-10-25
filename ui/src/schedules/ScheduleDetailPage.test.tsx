import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { makeSchedule } from "../test/schedules";
import { ScheduleDetailPage } from "./ScheduleDetailPage";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getSchedule: vi.fn(),
    listScheduleRuns: vi.fn(),
    getScheduleMemory: vi.fn(),
    forgetScheduleMemory: vi.fn(),
    updateSchedule: vi.fn(),
    runScheduleNow: vi.fn(),
    deleteSchedule: vi.fn(),
  };
});

const memory: api.ScheduleMemory = {
  findings: Array.from({ length: 6 }, (_, i) => ({
    key: `k${i}`,
    label: `Film ${i}`,
    sourceUrl: "",
    firstSeenAt: "2026-10-22T12:00:00Z",
  })),
  sources: [
    {
      url: "arena.ch/programm",
      prefer: "fetch",
      fetchFailedAt: null,
      browserOkAt: null,
      runs: 4,
      finds: 7,
      lastUsedAt: "2026-10-22T12:00:00Z",
    },
    {
      url: "pathe.ch/kinos",
      prefer: "browser",
      fetchFailedAt: "2026-10-08T12:00:00Z",
      browserOkAt: "2026-10-22T12:00:00Z",
      runs: 3,
      finds: 4,
      lastUsedAt: "2026-10-22T12:00:00Z",
    },
    {
      url: "imdb.com/calendar",
      prefer: "fetch",
      fetchFailedAt: null,
      browserOkAt: null,
      runs: 4,
      finds: 0,
      lastUsedAt: "2026-10-22T12:00:00Z",
    },
  ],
  queries: [
    {
      query: "neue kinofilme",
      runs: 4,
      finds: 5,
      emptyRuns: 0,
      lastUsedAt: "2026-10-22T12:00:00Z",
    },
    {
      query: "sci-fi kino",
      runs: 2,
      finds: 0,
      emptyRuns: 2,
      lastUsedAt: "2026-10-22T12:00:00Z",
    },
  ],
};

const runs: api.ScheduleRun[] = [
  makeSchedule().lastRun as api.ScheduleRun,
  {
    ...(makeSchedule().lastRun as api.ScheduleRun),
    id: "r2",
    status: "failed",
    newCount: 0,
    newLabels: [],
    error: "search provider unavailable",
    messageId: null,
  },
];

const props = () => ({
  scheduleID: "s1",
  onOpenSidebar: vi.fn(),
  onBack: vi.fn(),
  onOpenThread: vi.fn(),
  onSessionExpired: vi.fn(),
});

beforeEach(() => {
  vi.mocked(api.getSchedule).mockResolvedValue(makeSchedule());
  vi.mocked(api.listScheduleRuns).mockResolvedValue(runs);
  vi.mocked(api.getScheduleMemory).mockResolvedValue(memory);
  vi.mocked(api.forgetScheduleMemory).mockResolvedValue();
});

test("shows the schedule, its runs and its memory", async () => {
  const p = props();
  render(<ScheduleDetailPage {...p} />);
  expect(
    await screen.findByRole("heading", { name: "Cinema scout" }),
  ).toBeInTheDocument();
  expect(
    screen.getByText("6 reported · 3 sources · 2 searches"),
  ).toBeInTheDocument();
  expect(
    screen.getByText("Dune: Part Three, Avengers: Doomsday"),
  ).toBeInTheDocument();
  expect(screen.getByText("search provider unavailable")).toBeInTheDocument();
  expect(screen.getAllByText("41k").length).toBe(2);
  expect(screen.getAllByText("1m 48s").length).toBeGreaterThan(0);
  fireEvent.click(screen.getByRole("button", { name: "Open turn →" }));
  expect(p.onOpenThread).toHaveBeenCalledWith("th1");

  fireEvent.click(screen.getByRole("tab", { name: "Memory 11" }));
  expect(screen.getByText("Already reported")).toBeInTheDocument();
  expect(screen.queryByText("Film 5")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Show all 6" }));
  expect(screen.getByText("Film 5")).toBeInTheDocument();
  expect(screen.getByText("top source")).toBeInTheDocument();
  expect(screen.getByText("use browser")).toBeInTheDocument();
  expect(screen.getByText("no finds")).toBeInTheDocument();
  expect(screen.getByText("0 finds · 2 of 3")).toBeInTheDocument();

  fireEvent.click(screen.getAllByRole("button", { name: "Forget" })[0]);
  await waitFor(() =>
    expect(api.forgetScheduleMemory).toHaveBeenCalledWith(
      "s1",
      "findings",
      "k0",
    ),
  );
  fireEvent.click(screen.getAllByRole("button", { name: "Forget all" })[1]);
  await waitFor(() =>
    expect(api.forgetScheduleMemory).toHaveBeenCalledWith(
      "s1",
      "sources",
      undefined,
    ),
  );
});

test("an auto-disabled schedule offers to enable again", async () => {
  vi.mocked(api.getSchedule).mockResolvedValue(
    makeSchedule({
      enabled: false,
      disabledReason: "timeout",
      disabledAt: "2026-10-23T10:00:00Z",
    }),
  );
  vi.mocked(api.updateSchedule).mockResolvedValue(makeSchedule());
  render(<ScheduleDetailPage {...props()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Enable again" }));
  await waitFor(() =>
    expect(api.updateSchedule).toHaveBeenCalledWith("s1", { enabled: true }),
  );
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Enable again" }),
    ).not.toBeInTheDocument(),
  );
});

test("run now, edit, thread and delete", async () => {
  vi.mocked(api.runScheduleNow).mockResolvedValue();
  vi.mocked(api.deleteSchedule).mockResolvedValue();
  const p = props();
  render(<ScheduleDetailPage {...p} />);
  await screen.findByRole("heading", { name: "Cinema scout" });
  fireEvent.click(screen.getByRole("button", { name: /Run now/ }));
  expect(await screen.findByText(/Run started/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /^Thread/ }));
  expect(p.onOpenThread).toHaveBeenCalledWith("th1");
  fireEvent.click(screen.getByRole("button", { name: /Edit/ }));
  expect(
    await screen.findByRole("dialog", { name: "Edit schedule" }),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  fireEvent.click(screen.getAllByRole("button", { name: "Delete" })[1]);
  await waitFor(() => expect(p.onBack).toHaveBeenCalled());
});

test("a missing schedule says so", async () => {
  vi.mocked(api.getSchedule).mockRejectedValue(new Error("not found"));
  render(<ScheduleDetailPage {...props()} />);
  expect(await screen.findByText(/does not exist/)).toBeInTheDocument();
});

test("an empty memory says so", async () => {
  vi.mocked(api.getScheduleMemory).mockResolvedValue({
    findings: [],
    sources: [],
    queries: [],
  });
  vi.mocked(api.listScheduleRuns).mockResolvedValue([]);
  render(<ScheduleDetailPage {...props()} />);
  expect(await screen.findByText("No runs yet.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("tab", { name: "Memory 0" }));
  expect(screen.getByText("Nothing remembered yet.")).toBeInTheDocument();
});
