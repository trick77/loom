import "@testing-library/jest-dom/vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import * as api from "../api";
import { todayFixture, tomorrowFixture } from "../test/today";
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
      userID="u1"
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

describe("greeting", () => {
  const lastSeen = () => localStorage.getItem("loom.greeting.lastSeen.u1");
  const greeting = () => screen.getByRole("heading", { level: 2 }).textContent;

  beforeEach(() => {
    localStorage.clear();
    vi.mocked(api.getToday).mockRejectedValue(new Error("down"));
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("names the user on the first start screen of the day only", () => {
    const first = renderStart();
    expect(greeting()).toContain("Jan");
    expect(lastSeen()).not.toBeNull();
    first.unmount();

    renderStart();
    expect(greeting()).not.toContain("Jan");
  });

  test("a start screen opened in the background keeps the name for later", () => {
    const visibility = vi
      .spyOn(document, "visibilityState", "get")
      .mockReturnValue("hidden");
    renderStart();
    expect(lastSeen()).toBeNull();

    visibility.mockReturnValue("visible");
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(lastSeen()).not.toBeNull();
  });
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
    "Which place should I show the weather and sun times for?",
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
    todayFixture({ runs: [todayFixture().runs[1]] }),
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
  // Its own announcement reloads nothing: the card is already gone.
  await new Promise((r) => setTimeout(r, 20));
  expect(api.getToday).toHaveBeenCalledOnce();
  // The run left has no thread to open.
  expect(screen.queryByRole("button", { name: "Flats in Zürich" })).toBeNull();
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
  fireEvent.click(within(news).getByRole("button", { name: "Tech briefing" }));
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
  // Only the runs shown, never everything the bell holds.
  expect(api.markAllNotificationsRead).not.toHaveBeenCalled();
  expect(api.markNotificationRead).toHaveBeenCalledTimes(2);
  expect(api.markNotificationRead).toHaveBeenCalledWith("n1");
  expect(api.markNotificationRead).toHaveBeenCalledWith("n2");
  await waitFor(() =>
    expect(events).toEqual([
      api.NOTIFICATIONS_CHANGED,
      api.THREAD_NEWS_CHANGED,
    ]),
  );
  expect(starters()).toBeInTheDocument();
  expect(api.getToday).toHaveBeenCalledOnce();
});

test("a failed mark-all brings back only the cards that failed", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture());
  vi.mocked(api.markNotificationRead).mockImplementation(async (id) => {
    if (id === "n2") throw new Error("down");
  });
  renderStart();
  const news = await screen.findByRole("region", {
    name: /New from your schedules/,
  });
  fireEvent.click(
    within(news).getByRole("button", { name: "Mark all as read" }),
  );
  const back = await screen.findByRole("region", {
    name: /New from your schedules/,
  });
  expect(within(back).getAllByRole("article")).toHaveLength(1);
  expect(within(back).getByRole("article")).toHaveTextContent(
    "Flats in Zürich",
  );
  await waitFor(() =>
    expect(events).toEqual([
      api.NOTIFICATIONS_CHANGED,
      api.THREAD_NEWS_CHANGED,
    ]),
  );
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
  expect(screen.getByRole("region", { name: /^Today · / })).toBeInTheDocument();
});

test("news the bell read or found reloads today", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture());
  renderStart();
  await screen.findByRole("region", { name: /New from your schedules/ });
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
  act(() => api.announce(api.THREAD_NEWS_CHANGED));
  await waitFor(() => expect(screen.queryAllByRole("article")).toHaveLength(0));
  expect(api.getToday).toHaveBeenCalledTimes(2);
});

test("a reload shows the day the server now shows", async () => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
  renderStart();
  await screen.findByRole("region", { name: "Today · Monday, November 9" });
  vi.mocked(api.getToday).mockResolvedValue(tomorrowFixture({ runs: [] }));
  act(() => api.announce(api.NOTIFICATIONS_CHANGED));
  expect(
    await screen.findByRole("region", {
      name: "Tomorrow · Tuesday, November 10",
    }),
  ).toBeInTheDocument();
});

test("an older answer never overwrites a newer one", async () => {
  let resolveFirst: (today: api.Today) => void = () => {};
  vi.mocked(api.getToday).mockReturnValueOnce(
    new Promise((r) => {
      resolveFirst = r;
    }),
  );
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
  renderStart();
  await waitFor(() => expect(api.getToday).toHaveBeenCalledOnce());
  act(() => api.announce(api.NOTIFICATIONS_CHANGED));
  await screen.findByRole("region", { name: /^Today · / });
  await act(async () => resolveFirst(todayFixture()));
  expect(screen.queryAllByRole("article")).toHaveLength(0);
});

test("a phone asks the same and gets the tiles", async () => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: true,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  try {
    vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
    renderStart();
    expect(
      await screen.findByRole("list", { name: "Next hours" }),
    ).toBeInTheDocument();
    expect(api.getToday).toHaveBeenCalledWith();
  } finally {
    vi.unstubAllGlobals();
  }
});

describe("returning to the tab", () => {
  let visibility: DocumentVisibilityState = "visible";

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(2026, 10, 9, 10, 0));
    visibility = "visible";
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => visibility,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    delete (document as { visibilityState?: unknown }).visibilityState;
  });

  function comeBack(at: Date) {
    vi.setSystemTime(at);
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
  }

  test("retries a first load that failed", async () => {
    vi.mocked(api.getToday).mockRejectedValueOnce(new Error("down"));
    renderStart();
    await waitFor(() => expect(starters()).toBeInTheDocument());
    vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
    comeBack(new Date(2026, 10, 9, 10, 1));
    expect(
      await screen.findByRole("region", { name: /^Today · / }),
    ).toBeInTheDocument();
    expect(api.getToday).toHaveBeenCalledTimes(2);
    expect(api.getToday).toHaveBeenCalledWith();
  });

  test("reloads only when the last load is stale", async () => {
    vi.mocked(api.getToday).mockResolvedValue(todayFixture({ runs: [] }));
    renderStart();
    await screen.findByRole("region", { name: /^Today · / });

    comeBack(new Date(2026, 10, 9, 10, 20));
    visibility = "hidden";
    comeBack(new Date(2026, 10, 9, 11, 0));
    await new Promise((r) => setTimeout(r, 20));
    expect(api.getToday).toHaveBeenCalledOnce();

    visibility = "visible";
    comeBack(new Date(2026, 10, 9, 10, 31));
    await waitFor(() => expect(api.getToday).toHaveBeenCalledTimes(2));
    expect(
      screen.getByRole("region", { name: /^Today · / }),
    ).toBeInTheDocument();
  });

  test("a passed refreshAt reloads, even within half an hour", async () => {
    // A sleeping tab can miss the timer; coming back catches up.
    vi.setSystemTime(new Date(2026, 10, 9, 17, 50));
    vi.mocked(api.getToday).mockResolvedValue(
      todayFixture({
        runs: [],
        refreshAt: new Date(2026, 10, 9, 18, 0).toISOString(),
      }),
    );
    renderStart();
    await screen.findByRole("region", { name: /^Today · / });
    vi.mocked(api.getToday).mockResolvedValue(tomorrowFixture({ runs: [] }));
    comeBack(new Date(2026, 10, 9, 17, 55));
    await new Promise((r) => setTimeout(r, 20));
    expect(api.getToday).toHaveBeenCalledOnce();
    comeBack(new Date(2026, 10, 9, 18, 5));
    expect(
      await screen.findByRole("region", {
        name: "Tomorrow · Tuesday, November 10",
      }),
    ).toBeInTheDocument();
  });
});

describe("the shown day's end", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout"] });
    vi.setSystemTime(new Date(2026, 10, 9, 17, 59));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // tick runs the clock and lets the loads it starts answer.
  async function tick(ms: number) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms);
    });
  }

  test("reloads at refreshAt and shows tomorrow", async () => {
    vi.mocked(api.getToday).mockResolvedValue(
      todayFixture({
        runs: [],
        refreshAt: new Date(2026, 10, 9, 18, 0).toISOString(),
      }),
    );
    renderStart();
    await tick(0);
    expect(
      screen.getByRole("region", { name: /^Today · / }),
    ).toBeInTheDocument();
    vi.mocked(api.getToday).mockResolvedValue(tomorrowFixture({ runs: [] }));
    await tick(59_000);
    expect(api.getToday).toHaveBeenCalledOnce();
    await tick(1_000);
    expect(api.getToday).toHaveBeenCalledTimes(2);
    expect(
      screen.getByRole("region", { name: "Tomorrow · Tuesday, November 10" }),
    ).toBeInTheDocument();
  });

  test.each([
    ["past", new Date(2026, 10, 9, 17, 0).toISOString()],
    ["absent", null],
    ["not a time", "soon"],
  ])("a %s refreshAt schedules nothing", async (_, refreshAt) => {
    vi.mocked(api.getToday).mockResolvedValue(
      todayFixture({ runs: [], refreshAt }),
    );
    renderStart();
    await tick(0);
    expect(api.getToday).toHaveBeenCalledOnce();
    await tick(12 * 60 * 60_000);
    expect(api.getToday).toHaveBeenCalledOnce();
  });

  test("a far refreshAt is capped at six hours", async () => {
    vi.mocked(api.getToday).mockResolvedValue(
      todayFixture({
        runs: [],
        refreshAt: new Date(2026, 10, 10, 18, 0).toISOString(),
      }),
    );
    renderStart();
    await tick(0);
    await tick(6 * 60 * 60_000 - 1);
    expect(api.getToday).toHaveBeenCalledOnce();
    await tick(1);
    expect(api.getToday).toHaveBeenCalledTimes(2);
  });

  test("leaving the start page drops the timer", async () => {
    vi.mocked(api.getToday).mockResolvedValue(
      todayFixture({
        runs: [],
        refreshAt: new Date(2026, 10, 9, 18, 0).toISOString(),
      }),
    );
    const { unmount } = renderStart();
    await tick(0);
    unmount();
    await tick(60_000);
    expect(api.getToday).toHaveBeenCalledOnce();
  });
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
