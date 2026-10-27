import "@testing-library/jest-dom/vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { NotificationBell } from "./NotificationBell";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    unreadNotificationCount: vi.fn(),
    listNotifications: vi.fn(),
    markNotificationRead: vi.fn(),
    markAllNotificationsRead: vi.fn(),
  };
});

const run: api.AppNotification = {
  id: "n1",
  kind: "schedule_run",
  scheduleId: "s1",
  threadId: "th1",
  messageId: "m1",
  title: "Cinema scout",
  body: "Dune: Part Three opens 17 Dec at Arena, Avengers: Doomsday follows a week later.",
  createdAt: new Date().toISOString(),
  readAt: null,
};
const disabled: api.AppNotification = {
  ...run,
  id: "n2",
  kind: "schedule_disabled",
  title: "Hallenstadion tickets was disabled",
  body: "Two runs in a row took longer than 5 minutes.",
  readAt: "2026-10-23T10:00:00Z",
};

beforeEach(() => {
  vi.mocked(api.markNotificationRead).mockResolvedValue();
  vi.mocked(api.markAllNotificationsRead).mockResolvedValue();
  vi.mocked(api.listNotifications).mockResolvedValue([run, disabled]);
});

test("shows a dot while anything is unread, the inbox on click", async () => {
  vi.mocked(api.unreadNotificationCount).mockResolvedValue(1);
  const onOpen = vi.fn();
  const onManage = vi.fn();
  render(
    <NotificationBell
      placement="header"
      onOpen={onOpen}
      onManageSchedules={onManage}
    />,
  );
  expect(await screen.findByTestId("notification-dot")).toBeInTheDocument();

  fireEvent.click(
    screen.getByRole("button", { name: /Notifications, some unread/ }),
  );
  const panel = await screen.findByRole("dialog", { name: "Notifications" });
  expect(panel).toBeInTheDocument();
  expect(await screen.findByText("Cinema scout")).toBeInTheDocument();
  expect(
    screen.getByText("Hallenstadion tickets was disabled"),
  ).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Unread 1" }));
  expect(
    screen.queryByText("Hallenstadion tickets was disabled"),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "All" }));

  fireEvent.click(screen.getByText("Cinema scout"));
  expect(onOpen).toHaveBeenCalledWith(run);
  expect(api.markNotificationRead).toHaveBeenCalledWith("n1");
  await waitFor(() =>
    expect(screen.queryByTestId("notification-dot")).not.toBeInTheDocument(),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Notifications" }));
  fireEvent.click(
    await screen.findByRole("button", { name: /Manage schedules/ }),
  );
  expect(onManage).toHaveBeenCalled();
});

test("mark all read clears the dot", async () => {
  vi.mocked(api.unreadNotificationCount).mockResolvedValue(1);
  render(
    <NotificationBell
      placement="rail"
      onOpen={vi.fn()}
      onManageSchedules={vi.fn()}
    />,
  );
  await screen.findByTestId("notification-dot");
  fireEvent.click(screen.getByRole("button", { name: /some unread/ }));
  fireEvent.click(await screen.findByRole("button", { name: "Mark all read" }));
  expect(api.markAllNotificationsRead).toHaveBeenCalled();
  expect(screen.queryByTestId("notification-dot")).not.toBeInTheDocument();
  fireEvent.pointerDown(document.body);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});

test("an empty inbox says so", async () => {
  vi.mocked(api.unreadNotificationCount).mockResolvedValue(0);
  vi.mocked(api.listNotifications).mockResolvedValue([]);
  render(
    <NotificationBell
      placement="header"
      onOpen={vi.fn()}
      onManageSchedules={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Notifications" }));
  expect(await screen.findByText("You're all caught up")).toBeInTheDocument();
});

test("a failed inbox load says so", async () => {
  vi.mocked(api.unreadNotificationCount).mockRejectedValue(new Error("down"));
  vi.mocked(api.listNotifications).mockRejectedValue(new Error("down"));
  render(
    <NotificationBell
      placement="header"
      onOpen={vi.fn()}
      onManageSchedules={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Notifications" }));
  expect(
    await screen.findByText("Notifications failed to load."),
  ).toBeInTheDocument();
});

test("a toast stays up until it is dismissed", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  try {
    vi.mocked(api.unreadNotificationCount)
      .mockResolvedValueOnce(0)
      .mockResolvedValue(1);
    render(
      <NotificationBell
        placement="header"
        onOpen={vi.fn()}
        onManageSchedules={vi.fn()}
      />,
    );
    await waitFor(() =>
      expect(api.unreadNotificationCount).toHaveBeenCalledTimes(1),
    );
    await act(() => new Promise((resolve) => setTimeout(resolve, 20)));
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await screen.findByRole("status");

    await act(async () => {
      vi.advanceTimersByTime(30_000);
    });
    expect(screen.getByRole("status")).toHaveTextContent("Cinema scout");

    fireEvent.click(screen.getAllByRole("button", { name: "Dismiss" })[0]);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});

// showToast renders the bell and lets a focus poll find one new notification.
async function showToast(...later: number[]) {
  const counts = vi.mocked(api.unreadNotificationCount).mockResolvedValueOnce(0);
  for (const n of [1, ...later]) counts.mockResolvedValueOnce(n);
  render(
    <NotificationBell
      placement="header"
      onOpen={vi.fn()}
      onManageSchedules={vi.fn()}
    />,
  );
  await waitFor(() =>
    expect(api.unreadNotificationCount).toHaveBeenCalledTimes(1),
  );
  await act(() => new Promise((resolve) => setTimeout(resolve, 20)));
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  await screen.findByRole("status");
}

test("news read elsewhere takes the toast with it", async () => {
  vi.mocked(api.unreadNotificationCount).mockReset();
  await showToast(0);
  await act(async () => {
    window.dispatchEvent(new Event(api.NOTIFICATIONS_CHANGED));
  });
  await waitFor(() =>
    expect(screen.queryByRole("status")).not.toBeInTheDocument(),
  );
});

test("mark all read takes the toast with it", async () => {
  vi.mocked(api.unreadNotificationCount).mockReset();
  await showToast();
  fireEvent.click(screen.getByRole("button", { name: /some unread/ }));
  fireEvent.click(await screen.findByRole("button", { name: "Mark all read" }));
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});

test("escape dismisses the toast", async () => {
  vi.mocked(api.unreadNotificationCount).mockReset();
  await showToast();
  fireEvent.keyDown(window, { key: "Escape" });
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});

test("a poll that finds something new shows a toast", async () => {
  vi.mocked(api.unreadNotificationCount)
    .mockResolvedValueOnce(0)
    .mockResolvedValueOnce(1)
    .mockResolvedValueOnce(3);
  const onOpen = vi.fn();
  render(
    <NotificationBell
      placement="header"
      onOpen={onOpen}
      onManageSchedules={vi.fn()}
    />,
  );
  await waitFor(() =>
    expect(api.unreadNotificationCount).toHaveBeenCalledTimes(1),
  );
  // Let the first count land before the next poll compares against it.
  await act(() => new Promise((resolve) => setTimeout(resolve, 20)));
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  const toast = await screen.findByRole("status");
  expect(toast).toHaveTextContent("Cinema scout");
  fireEvent.click(screen.getByRole("button", { name: "Open" }));
  expect(onOpen).toHaveBeenCalledWith(run);
  expect(screen.queryByRole("status")).not.toBeInTheDocument();

  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  expect(await screen.findByText("3 new notifications")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Open inbox" }));
  expect(
    await screen.findByRole("dialog", { name: "Notifications" }),
  ).toBeInTheDocument();
});

test("focus polls again", async () => {
  vi.mocked(api.unreadNotificationCount).mockResolvedValue(0);
  render(
    <NotificationBell
      placement="header"
      onOpen={vi.fn()}
      onManageSchedules={vi.fn()}
    />,
  );
  await waitFor(() => expect(api.unreadNotificationCount).toHaveBeenCalled());
  const calls = vi.mocked(api.unreadNotificationCount).mock.calls.length;
  window.dispatchEvent(new Event("focus"));
  await waitFor(() =>
    expect(vi.mocked(api.unreadNotificationCount).mock.calls.length).toBe(
      calls + 1,
    ),
  );
});
