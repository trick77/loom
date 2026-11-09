import "@testing-library/jest-dom/vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { UserFacingError } from "../api/http";
import { todayFixture, tomorrowFixture } from "../test/today";
import { TodaySection } from "./TodaySection";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return { ...actual, setLocation: vi.fn() };
});

const handlers = {
  onOpenThread: vi.fn(),
  onOpenMemory: vi.fn(),
  onMarkRead: vi.fn(),
  onMarkAllRead: vi.fn(),
  onReload: vi.fn(),
};

function show(overrides: Partial<api.Today> = {}) {
  return render(<TodaySection today={todayFixture(overrides)} {...handlers} />);
}

function showTomorrow(overrides: Partial<api.Today> = {}) {
  return render(
    <TodaySection today={tomorrowFixture(overrides)} {...handlers} />,
  );
}

const hourItems = () =>
  within(screen.getByRole("list", { name: "Next hours" })).getAllByRole(
    "listitem",
  );

beforeEach(() => {
  for (const handler of Object.values(handlers)) handler.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("today shows the weather and sun tiles, nothing to fold", () => {
  show();
  const section = screen.getByRole("region", {
    name: "Today · Monday, November 9",
  });
  expect(
    within(section).getByRole("button", {
      name: "Zürich · from your memories",
    }),
  ).toBeInTheDocument();
  // The place chip is the only disclosure: no line to expand.
  const toggles = within(section)
    .getAllByRole("button")
    .filter((button) => button.hasAttribute("aria-expanded"));
  expect(toggles).toHaveLength(1);
  expect(within(section).getByText("10°")).toBeInTheDocument();
  expect(
    within(section).getByRole("img", { name: "Partly cloudy" }),
  ).toBeInTheDocument();
  expect(
    within(section).getByText("H 11° · L 3° · Rain 10%"),
  ).toBeInTheDocument();
  expect(hourItems()).toHaveLength(5);
  // 21:00 is after sunset: the clear sky is a moon.
  expect(hourItems()[4]).toHaveAttribute("title", "Clear");
  expect(within(section).getByText("07:25")).toBeInTheDocument();
  expect(within(section).getByText("16:58")).toBeInTheDocument();
  expect(
    within(section).getByText(
      "Day length 9 h 33 min · 2 min shorter than yesterday",
    ),
  ).toBeInTheDocument();
  expect(
    within(section).getByRole("img", {
      name: "Sun position: 6 h 41 min of daylight left",
    }),
  ).toBeInTheDocument();
  expect(within(section).getByTestId("sun-dot")).toBeInTheDocument();
  const credit = within(section).getByRole("link", {
    name: "Weather data by Open-Meteo.com",
  });
  expect(credit).toHaveAttribute("href", "https://open-meteo.com/");
  expect(credit).toHaveAttribute("target", "_blank");
  expect(credit).toHaveAttribute("rel", "noopener");
});

test("a phone gets the same tiles", () => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: true,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  show();
  expect(screen.getByRole("region", { name: /^Today · / })).toBeInTheDocument();
  expect(hourItems()).toHaveLength(5);
  expect(screen.getByText("07:25")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /°/ })).toBeNull();
});

test("from the evening on, tomorrow: its range, sky and sun times", () => {
  showTomorrow();
  const section = screen.getByRole("region", {
    name: "Tomorrow · Tuesday, November 10",
  });
  expect(within(section).getByTestId("day-range")).toHaveTextContent(
    "12° / 4°",
  );
  expect(
    within(section).getByRole("img", { name: "Cloudy" }),
  ).toBeInTheDocument();
  expect(within(section).getByText("Rain 40%")).toBeInTheDocument();
  expect(within(section).queryByText(/^H /)).toBeNull();
  expect(hourItems()[0]).toHaveTextContent("06:00");
  // No "now" tomorrow: the arc has no sun on it.
  expect(within(section).queryByTestId("sun-dot")).toBeNull();
  expect(
    within(section).getByRole("img", {
      name: "Sunrise 07:25, sunset 16:58",
    }),
  ).toBeInTheDocument();
  expect(
    within(section).getByText(
      "Day length 9 h 33 min · 2 min shorter than today",
    ),
  ).toBeInTheDocument();
});

test("tomorrow without a rain chance, longer or as long as today", () => {
  const base = tomorrowFixture();
  const { unmount } = showTomorrow({
    sun: { ...base.sun!, deltaMinutes: 1 },
    weather: {
      ...base.weather!,
      day: { ...base.weather!.day, precipitationProbability: null },
    },
  });
  expect(screen.queryByText(/^Rain/)).toBeNull();
  expect(
    screen.getByText("Day length 9 h 33 min · 1 min longer than today"),
  ).toBeInTheDocument();
  unmount();
  showTomorrow({ sun: { ...base.sun!, deltaMinutes: 0 } });
  expect(
    screen.getByText("Day length 9 h 33 min · same as today"),
  ).toBeInTheDocument();
});

test("a warning stands out in the tile, today and tomorrow", () => {
  const { unmount } = show({ warning: { kind: "rain", from: "17:00" } });
  const warning = screen.getByTestId("weather-warning");
  expect(warning).toHaveTextContent("Rain from 17:00");
  expect(warning).toHaveClass("text-accent-strong");
  expect(warning.querySelector("svg")).toHaveAttribute("data-warning", "rain");
  // The rainy 18:00 slot is drawn in the accent, the sunny ones are not.
  expect(hourItems()[3].querySelector("svg")).toHaveClass("text-accent-strong");
  expect(hourItems()[1].querySelector("svg")).toHaveClass("text-sun");
  unmount();

  showTomorrow({ warning: { kind: "rain", from: "09:00" } });
  expect(screen.getByTestId("weather-warning")).toHaveTextContent(
    "Rain tomorrow from 09:00",
  );
});

test.each([
  ["frost", "Frost from 06:00"],
  ["snow", "Snow from 06:00"],
  ["storm", "Thunderstorms from 06:00"],
] as const)("a %s warning draws its own icon", (kind, text) => {
  show({ warning: { kind, from: "06:00" } });
  const warning = screen.getByTestId("weather-warning");
  expect(warning).toHaveTextContent(text);
  expect(warning.querySelector("svg")).toHaveAttribute("data-warning", kind);
});

test("the place chip explains the memory and opens the memories", () => {
  show();
  fireEvent.click(
    screen.getByRole("button", { name: "Zürich · from your memories" }),
  );
  const dialog = screen.getByRole("dialog", {
    name: "Location for weather and sun",
  });
  expect(dialog).toHaveTextContent("From your memories: you live in Zürich.");
  expect(dialog).toHaveTextContent("I'm in Lisbon this week");
  fireEvent.keyDown(window, { key: "Escape" });
  expect(screen.queryByRole("dialog")).toBeNull();

  fireEvent.click(
    screen.getByRole("button", { name: "Zürich · from your memories" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Open memories" }));
  expect(handlers.onOpenMemory).toHaveBeenCalledOnce();
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("a stay elsewhere reads as the current place", () => {
  show({
    weather: null,
    runs: [],
    location: { name: "Lisbon", kind: "current", until: "2026-11-15" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Lisbon" }));
  expect(screen.getByRole("dialog")).toHaveTextContent(
    "you're in Lisbon right now.",
  );
  fireEvent.pointerDown(document.body);
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("before sunrise the sun is not on its arc", () => {
  const sun = todayFixture().sun!;
  show({ sun: { ...sun, progress: -0.2, deltaMinutes: 3 } });
  expect(
    screen.getByRole("img", { name: "Sun position: not up yet" }),
  ).toBeInTheDocument();
  expect(screen.queryByTestId("sun-dot")).toBeNull();
  expect(
    screen.getByText("Day length 9 h 33 min · 3 min longer than yesterday"),
  ).toBeInTheDocument();
});

test("after sunset the sun is set; the polar day keeps it up", () => {
  const sun = todayFixture().sun!;
  const { unmount } = show({ sun: { ...sun, progress: 1.3, deltaMinutes: 0 } });
  expect(
    screen.getByRole("img", { name: "Sun position: set" }),
  ).toBeInTheDocument();
  expect(
    screen.getByText("Day length 9 h 33 min · same as yesterday"),
  ).toBeInTheDocument();
  unmount();
  show({
    sun: {
      ...sun,
      rise: null,
      set: null,
      polar: "day",
      dayMinutes: 1440,
      deltaMinutes: 0,
    },
  });
  expect(screen.getAllByText("Polar day").length).toBeGreaterThan(0);
  expect(screen.getByTestId("sun-dot")).toBeInTheDocument();
});

test("without weather the sun times show alone", () => {
  const sun = todayFixture().sun!;
  const { unmount } = show({ weather: null, runs: [] });
  const group = screen.getByRole("group", { name: "Today" });
  expect(group).toHaveTextContent("Sunrise 07:25");
  expect(group).toHaveTextContent("Sunset 16:58");
  expect(screen.queryByRole("link")).toBeNull();
  unmount();

  show({
    weather: null,
    runs: [],
    sun: { ...sun, rise: null, set: null, polar: "night", dayMinutes: 0 },
  });
  expect(screen.getByRole("group", { name: "Today" })).toHaveTextContent(
    "Polar night",
  );
});

test("in the evening without weather the sun line is tomorrow's", () => {
  const sun = tomorrowFixture().sun!;
  const { unmount } = showTomorrow({ weather: null, runs: [] });
  expect(screen.queryByRole("group", { name: "Today" })).toBeNull();
  const group = screen.getByRole("group", { name: "Tomorrow" });
  expect(within(group).getByTestId("sun-line-day")).toHaveTextContent(
    "Tomorrow",
  );
  expect(group).toHaveTextContent("Sunrise 07:25");
  expect(group).toHaveTextContent("Sunset 16:58");
  unmount();

  // Tomorrow is never "now": a stray position does not turn it into a
  // sunrise-next line.
  showTomorrow({
    weather: null,
    runs: [],
    sun: { ...sun, rise: null, progress: 1.3 },
  });
  const line = screen.getByRole("group", { name: "Tomorrow" });
  expect(line).toHaveTextContent("Sunset 16:58");
  // The sunset's icon is the setting one, not a sunrise arrow.
  expect(
    line.querySelector('path[d="M12 6v4M9.5 8.5 12 11l2.5-2.5"]'),
  ).not.toBeNull();
});

test("the sun line names the one sun time it knows", () => {
  const sun = todayFixture().sun!;
  const cases: [Partial<api.TodaySun>, string][] = [
    [{ rise: null }, "Sunset 16:58"],
    [{ set: null }, "Sunrise 07:25"],
    [{ set: null, progress: 1.3 }, "Sunrise 07:25"],
  ];
  for (const [change, text] of cases) {
    const { unmount } = show({
      weather: null,
      runs: [],
      sun: { ...sun, ...change },
    });
    expect(screen.getByRole("group", { name: "Today" })).toHaveTextContent(
      text,
    );
    unmount();
  }
  show({ weather: null, runs: [], sun: { ...sun, rise: null, set: null } });
  expect(screen.getByRole("group", { name: "Today" })).not.toHaveTextContent(
    /Sun/,
  );
});

test("without anything to show the section is empty", () => {
  const { container } = show({ weather: null, sun: null, runs: [] });
  expect(container).toBeEmptyDOMElement();
});

test("a credit link that is not a web address stays text", () => {
  const weather = todayFixture().weather!;
  show({
    weather: {
      ...weather,
      attribution: { text: "Weather data", url: "javascript:alert(1)" },
    },
  });
  expect(screen.getByText("Weather data")).toBeInTheDocument();
  expect(screen.queryByRole("link")).toBeNull();
});

test("unread runs open their thread or are marked read", () => {
  show();
  const news = screen.getByRole("region", { name: /New from your schedules/ });
  expect(within(news).getByTestId("news-count")).toHaveTextContent("2");
  expect(within(news).getByText("2 unread")).toBeInTheDocument();
  const cards = within(news).getAllByRole("article");
  expect(cards).toHaveLength(2);
  expect(cards[0]).toHaveTextContent("Tech briefing");
  expect(cards[0]).toHaveTextContent("one security fix");
  expect(cards[0]).toHaveTextContent("just now");
  // A run whose thread is gone has nothing to open.
  expect(
    within(cards[1]).queryByRole("button", { name: "Open full result →" }),
  ).toBeNull();

  fireEvent.click(
    within(cards[0]).getByRole("button", { name: "Open full result →" }),
  );
  expect(handlers.onOpenThread).toHaveBeenCalledWith("th1");
  fireEvent.click(
    within(cards[1]).getByRole("button", { name: "Mark as read" }),
  );
  expect(handlers.onMarkRead).toHaveBeenCalledWith("n2");
  fireEvent.click(
    within(news).getByRole("button", { name: "Mark all as read" }),
  );
  expect(handlers.onMarkAllRead).toHaveBeenCalledWith(["n1", "n2"]);
});

test("asks where the user lives and saves the place", async () => {
  vi.mocked(api.setLocation).mockResolvedValue({
    place: { name: "Munich", kind: "home" },
  });
  show({
    weather: null,
    sun: null,
    location: null,
    runs: [],
    askLocation: true,
  });
  const input = screen.getByLabelText(
    "Where do you live? Then I'll show the weather and sun times here.",
  );
  expect(
    screen.getByText(
      "Saved in your memories. You can change it any time in chat.",
    ),
  ).toBeInTheDocument();
  const save = screen.getByRole("button", { name: "Save" });
  expect(save).toBeDisabled();
  fireEvent.change(input, { target: { value: "  Munich " } });
  fireEvent.click(save);
  await waitFor(() => expect(handlers.onReload).toHaveBeenCalledOnce());
  expect(api.setLocation).toHaveBeenCalledWith("Munich");
});

test("an unknown place is said so; other failures too", async () => {
  vi.mocked(api.setLocation).mockRejectedValueOnce(
    new api.PlaceNotFoundError(),
  );
  show({
    weather: null,
    sun: null,
    location: null,
    runs: [],
    askLocation: true,
  });
  const input = screen.getByRole("textbox");
  fireEvent.change(input, { target: { value: "Atlantis" } });
  fireEvent.submit(input);
  expect(await screen.findByRole("alert")).toHaveTextContent("Place not found");
  expect(input).toHaveAttribute("aria-invalid", "true");
  expect(handlers.onReload).not.toHaveBeenCalled();

  vi.mocked(api.setLocation).mockRejectedValueOnce(
    new UserFacingError("Weather is off on this server."),
  );
  fireEvent.submit(input);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Weather is off on this server.",
  );

  vi.mocked(api.setLocation).mockRejectedValueOnce(new Error("boom"));
  fireEvent.submit(input);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Couldn't save the place. Try again.",
  );
});
