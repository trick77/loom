import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { makeSchedule } from "../test/schedules";
import { SchedulesPage } from "./SchedulesPage";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    listSchedules: vi.fn(),
    updateSchedule: vi.fn(),
    runScheduleNow: vi.fn(),
    deleteSchedule: vi.fn(),
    previewSchedule: vi.fn(),
    createSchedule: vi.fn(),
  };
});

const props = () => ({
  onOpenSidebar: vi.fn(),
  onOpenSchedule: vi.fn(),
  onOpenThread: vi.fn(),
  onSessionExpired: vi.fn(),
});

beforeEach(() => {
  vi.mocked(api.listSchedules).mockReset();
});

test("lists schedules with their slot and last run", async () => {
  vi.mocked(api.listSchedules).mockResolvedValue([
    makeSchedule(),
    makeSchedule({
      id: "s2",
      name: "Hallenstadion tickets",
      enabled: false,
      disabledReason: "timeout",
      disabledAt: "2026-10-23T10:00:00Z",
      lastRun: null,
    }),
    makeSchedule({
      id: "s3",
      name: "GPU prices",
      enabled: false,
      lastRun: null,
    }),
  ]);
  const p = props();
  render(<SchedulesPage {...p} />);
  expect(await screen.findByText("Cinema scout")).toBeInTheDocument();
  expect(screen.getByText("2 new")).toBeInTheDocument();
  expect(screen.getByText("disabled · timeout")).toBeInTheDocument();
  expect(screen.getByText("paused")).toBeInTheDocument();
  expect(
    screen.getByText(/Weekly on Thursday at 14:37 · next/),
  ).toBeInTheDocument();
  expect(
    screen.getByText(/two runs in a row took longer than 5 minutes/),
  ).toBeInTheDocument();

  fireEvent.click(screen.getByRole("tab", { name: "Paused 2" }));
  expect(screen.queryByText("Cinema scout")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("tab", { name: "Active 1" }));
  expect(screen.queryByText("GPU prices")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("tab", { name: "All 3" }));

  fireEvent.click(screen.getByText("Cinema scout"));
  expect(p.onOpenSchedule).toHaveBeenCalledWith("s1");
});

test("shows the empty state", async () => {
  vi.mocked(api.listSchedules).mockResolvedValue([]);
  render(<SchedulesPage {...props()} />);
  expect(await screen.findByText("No schedules yet")).toBeInTheDocument();
});

test("a load failure is shown, an expired session signs out", async () => {
  vi.mocked(api.listSchedules).mockRejectedValueOnce(new Error("boom"));
  const { unmount } = render(<SchedulesPage {...props()} />);
  expect(
    await screen.findByText("Schedules failed to load."),
  ).toBeInTheDocument();
  unmount();
  vi.mocked(api.listSchedules).mockRejectedValueOnce(
    new api.AuthExpiredError(),
  );
  const p = props();
  render(<SchedulesPage {...p} />);
  await waitFor(() => expect(p.onSessionExpired).toHaveBeenCalled());
});

test("toggles, runs now, opens the thread and deletes", async () => {
  vi.mocked(api.listSchedules).mockResolvedValue([makeSchedule()]);
  vi.mocked(api.updateSchedule).mockResolvedValue(
    makeSchedule({ enabled: false }),
  );
  vi.mocked(api.runScheduleNow).mockResolvedValue();
  vi.mocked(api.deleteSchedule).mockResolvedValue();
  const p = props();
  render(<SchedulesPage {...p} />);
  await screen.findByText("Cinema scout");

  fireEvent.click(screen.getByRole("switch", { name: "Pause schedule" }));
  await waitFor(() =>
    expect(api.updateSchedule).toHaveBeenCalledWith("s1", { enabled: false }),
  );
  expect(
    await screen.findByRole("switch", { name: "Resume schedule" }),
  ).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Schedule actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: /Run now/ }));
  expect(await screen.findByText(/Run started/)).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Schedule actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: /Open thread/ }));
  expect(p.onOpenThread).toHaveBeenCalledWith("th1");

  fireEvent.click(screen.getByRole("button", { name: "Schedule actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: /Delete/ }));
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  await waitFor(() =>
    expect(screen.queryByText("Cinema scout")).not.toBeInTheDocument(),
  );
});

test("a running schedule's row refreshes until its run lands", async () => {
  vi.mocked(api.listSchedules)
    .mockResolvedValueOnce([makeSchedule({ running: true, lastRun: null })])
    .mockResolvedValue([makeSchedule()]);
  render(<SchedulesPage {...props()} />);
  expect(await screen.findByText("running")).toBeInTheDocument();
  expect(
    await screen.findByText("2 new", {}, { timeout: 8000 }),
  ).toBeInTheDocument();
}, 10000);

test("a busy run now shows the server's message", async () => {
  vi.mocked(api.listSchedules).mockResolvedValue([makeSchedule()]);
  vi.mocked(api.runScheduleNow).mockRejectedValue(
    new (await import("../api/http")).UserFacingError("already running"),
  );
  render(<SchedulesPage {...props()} />);
  await screen.findByText("Cinema scout");
  fireEvent.click(screen.getByRole("button", { name: "Schedule actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: /Run now/ }));
  expect(await screen.findByText("already running")).toBeInTheDocument();
});

test("creates a schedule with the slot the preview showed", async () => {
  vi.mocked(api.listSchedules).mockResolvedValue([]);
  vi.mocked(api.previewSchedule).mockResolvedValue({
    interval: "once a week",
    template: "~ 7~22 * * ~",
    cron: "37 14 * * 4",
    timezone: "Europe/Zurich",
    summary: "Weekly on Thursday at 14:37",
    nextRuns: ["2026-10-29T13:37:00Z", "2026-11-05T13:37:00Z"],
  });
  vi.mocked(api.createSchedule).mockResolvedValue(
    makeSchedule({ lastRun: null }),
  );
  render(<SchedulesPage {...props()} />);
  await screen.findByText("No schedules yet");
  fireEvent.click(screen.getByRole("button", { name: /New schedule/ }));
  const dialog = await screen.findByRole("dialog");
  const promptField = dialog.querySelector("textarea") as HTMLTextAreaElement;
  expect(promptField.maxLength).toBe(2000);
  fireEvent.change(promptField, {
    target: { value: "Find new films" },
  });
  expect(screen.getByText("14 / 2000")).toBeInTheDocument();
  expect(screen.getByText(/A run is short/)).toBeInTheDocument();
  fireEvent.change(screen.getByPlaceholderText("e.g. once a week"), {
    target: { value: "once a week" },
  });
  expect(
    await screen.findByText(
      /Weekly on Thursday at 14:37/,
      {},
      { timeout: 3000 },
    ),
  ).toBeInTheDocument();
  expect(screen.getByText("37 14 * * 4")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(api.createSchedule).toHaveBeenCalledWith({
      name: "",
      prompt: "Find new films",
      interval: "once a week",
      template: "~ 7~22 * * ~",
      cron: "37 14 * * 4",
      timezone: "Europe/Zurich",
    }),
  );
  expect(await screen.findByText("Cinema scout")).toBeInTheDocument();
});

test("the form explains an interval it cannot schedule", async () => {
  vi.mocked(api.listSchedules).mockResolvedValue([]);
  vi.mocked(api.previewSchedule)
    .mockRejectedValueOnce(new api.ScheduleIntervalError("too_often", "x"))
    .mockRejectedValueOnce(new api.ScheduleIntervalError("not_understood", "x"))
    .mockRejectedValueOnce(new Error("down"));
  render(<SchedulesPage {...props()} />);
  await screen.findByText("No schedules yet");
  fireEvent.click(screen.getByRole("button", { name: /New schedule/ }));
  const when = screen.getByPlaceholderText("e.g. once a week");
  fireEvent.change(when, { target: { value: "every 5 minutes" } });
  expect(
    await screen.findByText("Too often", {}, { timeout: 3000 }),
  ).toBeInTheDocument();
  fireEvent.change(when, { target: { value: "when it rains" } });
  expect(
    await screen.findByText("Not a time I can schedule", {}, { timeout: 3000 }),
  ).toBeInTheDocument();
  fireEvent.change(when, { target: { value: "daily" } });
  expect(
    await screen.findByText(/could not be read/, {}, { timeout: 3000 }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
