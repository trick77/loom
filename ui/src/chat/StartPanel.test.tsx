import "@testing-library/jest-dom/vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { todayFixture } from "../test/today";
import { StartPanel } from "./StartPanel";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getToday: vi.fn(),
    setLocation: vi.fn(),
    markNotificationRead: vi.fn(),
    markAllNotificationsRead: vi.fn(),
  };
});

// The composer and the window drop zone have their own tests; here they only
// take up their place.
vi.mock("./Composer", () => ({
  Composer: () => <div data-testid="composer" />,
}));
vi.mock("./WindowFileDrop", () => ({ WindowFileDrop: () => null }));

const onOpenThread = vi.fn();
const onOpenMemory = vi.fn();

function renderStart(
  overrides: Partial<{ draft: string; attachments: [] }> = {},
) {
  return render(
    <StartPanel
      displayName="Jan"
      draft={overrides.draft ?? ""}
      sendError=""
      attachments={overrides.attachments ?? []}
      attachNote=""
      onOpenSidebar={vi.fn()}
      onDraftChange={vi.fn()}
      pastedTexts={[]}
      onAddPastedText={vi.fn()}
      onRemovePastedText={vi.fn()}
      onSend={vi.fn()}
      onStop={vi.fn()}
      onAttachFiles={vi.fn()}
      onAttachError={vi.fn()}
      onRemoveAttachment={vi.fn()}
      onEnterIncognito={vi.fn()}
      onOpenThread={onOpenThread}
      onOpenMemory={onOpenMemory}
    />,
  );
}

const starters = () =>
  screen.queryByRole("list", { name: "Prompt categories" });

let events: string[] = [];
const record = (event: Event) => events.push(event.type);

beforeEach(() => {
  vi.mocked(api.getToday).mockReset();
  vi.mocked(api.markNotificationRead).mockReset().mockResolvedValue();
  vi.mocked(api.markAllNotificationsRead).mockReset().mockResolvedValue();
  onOpenThread.mockReset();
  events = [];
  window.addEventListener(api.NOTIFICATIONS_CHANGED, record);
  window.addEventListener(api.THREAD_NEWS_CHANGED, record);
});

afterEach(() => {
  window.removeEventListener(api.NOTIFICATIONS_CHANGED, record);
  window.removeEventListener(api.THREAD_NEWS_CHANGED, record);
});

test("shows no starters while today loads", async () => {
  let resolve: (today: api.Today) => void = () => {};
  vi.mocked(api.getToday).mockReturnValue(
    new Promise((r) => {
      resolve = r;
    }),
  );
  renderStart();
  await waitFor(() => expect(api.getToday).toHaveBeenCalledOnce());
  expect(starters()).toBeNull();
  expect(screen.getByTestId("composer")).toBeInTheDocument();
  await act(async () => resolve(todayFixture({ weather: null, runs: [] })));
  expect(starters()).toBeInTheDocument();
});

test("shows the starters when today failed to load", async () => {
  vi.mocked(api.getToday).mockRejectedValue(new Error("down"));
  renderStart();
  await waitFor(() => expect(starters()).toBeInTheDocument());
  expect(screen.queryByRole("region")).toBeNull();
});

test("weather or news take the starters' place", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
  const { unmount } = renderStart();
  expect(
    await screen.findByRole("region", { name: /^Today · / }),
  ).toBeInTheDocument();
  expect(starters()).toBeNull();
  unmount();

  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ weather: null }));
  renderStart();
  expect(
    await screen.findByRole("region", { name: /New from your schedules/ }),
  ).toBeInTheDocument();
  expect(starters()).toBeNull();
});

test("with neither weather nor news: starters, the sun line, the place question", async () => {
  vi.mocked(api.getToday).mockResolvedValue(
    todayFixture({ weather: null, runs: [], askLocation: true }),
  );
  renderStart();
  await waitFor(() => expect(starters()).toBeInTheDocument());
  const sunLine = screen.getByRole("group", { name: "Today" });
  const ask = screen.getByLabelText(
    "Where do you live? Then I'll show the weather and sun times here.",
  );
  // Both sit under the starters.
  expect(
    starters()!.compareDocumentPosition(sunLine) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(
    starters()!.compareDocumentPosition(ask) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

test("a draft hides the starters but keeps today", async () => {
  vi.mocked(api.getToday).mockResolvedValue(
    todayFixture({ weather: null, runs: [] }),
  );
  renderStart({ draft: "hello" });
  expect(
    await screen.findByRole("group", { name: "Today" }),
  ).toBeInTheDocument();
  expect(starters()).toBeNull();
});

test("marking a run read removes its card and tells the bell and sidebar", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture());
  renderStart();
  const news = await screen.findByRole("region", {
    name: /New from your schedules/,
  });
  vi.mocked(api.getToday).mockResolvedValue(
    todayFixture({ view: "line", runs: [todayFixture().runs[1]] }),
  );
  const first = within(news).getAllByRole("article")[0];
  fireEvent.click(within(first).getByRole("button", { name: "Mark as read" }));
  expect(within(news).getAllByRole("article")).toHaveLength(1);
  expect(api.markNotificationRead).toHaveBeenCalledWith("n1");
  await waitFor(() =>
    expect(events).toEqual([
      api.NOTIFICATIONS_CHANGED,
      api.THREAD_NEWS_CHANGED,
    ]),
  );
  // The reload the announcement brings keeps this visit's tiles.
  await waitFor(() => expect(api.getToday).toHaveBeenCalledTimes(2));
  expect(screen.getByRole("region", { name: /^Today · / })).toBeInTheDocument();
  // The run left has no thread to open.
  expect(
    screen.queryByRole("button", { name: "Open full result →" }),
  ).toBeNull();
});

test("a failed read brings the card back", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture());
  vi.mocked(api.markNotificationRead).mockRejectedValue(new Error("down"));
  renderStart();
  const news = await screen.findByRole("region", {
    name: /New from your schedules/,
  });
  fireEvent.click(
    within(news).getAllByRole("button", { name: "Mark as read" })[0],
  );
  await waitFor(() =>
    expect(within(news).getAllByRole("article")).toHaveLength(2),
  );
  expect(events).toEqual([]);
});

test("mark all read clears the news and the starters come back", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ weather: null }));
  renderStart();
  const news = await screen.findByRole("region", {
    name: /New from your schedules/,
  });
  fireEvent.click(
    within(news).getByRole("button", { name: "Open full result →" }),
  );
  expect(onOpenThread).toHaveBeenCalledWith("th1");
  vi.mocked(api.getToday).mockResolvedValue(
    todayFixture({ weather: null, runs: [] }),
  );
  fireEvent.click(
    within(news).getByRole("button", { name: "Mark all as read" }),
  );
  expect(
    screen.queryByRole("region", { name: /New from your schedules/ }),
  ).toBeNull();
  expect(api.markAllNotificationsRead).toHaveBeenCalledOnce();
  await waitFor(() =>
    expect(events).toEqual([
      api.NOTIFICATIONS_CHANGED,
      api.THREAD_NEWS_CHANGED,
    ]),
  );
  expect(starters()).toBeInTheDocument();
});

test("a failed mark-all brings the cards back", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture());
  vi.mocked(api.markAllNotificationsRead).mockRejectedValue(new Error("down"));
  renderStart();
  const news = await screen.findByRole("region", {
    name: /New from your schedules/,
  });
  fireEvent.click(
    within(news).getByRole("button", { name: "Mark all as read" }),
  );
  expect(
    await screen.findByRole("region", { name: /New from your schedules/ }),
  ).toBeInTheDocument();
  expect(screen.getAllByRole("article")).toHaveLength(2);
});

test("news read elsewhere reloads today; a failed reload keeps it", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture());
  renderStart();
  await screen.findByRole("region", { name: /New from your schedules/ });
  vi.mocked(api.getToday).mockRejectedValueOnce(new Error("down"));
  act(() => api.announce(api.NOTIFICATIONS_CHANGED));
  await waitFor(() => expect(api.getToday).toHaveBeenCalledTimes(2));
  expect(screen.getAllByRole("article")).toHaveLength(2);

  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
  act(() => api.announce(api.NOTIFICATIONS_CHANGED));
  await waitFor(() => expect(screen.queryAllByRole("article")).toHaveLength(0));
});

test("a saved place reloads today with its weather", async () => {
  vi.mocked(api.getToday).mockResolvedValue(
    todayFixture({
      weather: null,
      sun: null,
      location: null,
      runs: [],
      askLocation: true,
    }),
  );
  vi.mocked(api.setLocation).mockResolvedValue({
    place: { name: "Zürich", kind: "home" },
  });
  renderStart();
  const input = await screen.findByRole("textbox");
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
  fireEvent.change(input, { target: { value: "Zürich" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(
    await screen.findByRole("region", { name: /^Today · / }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(starters()).toBeNull();
});
