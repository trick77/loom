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
import { todayFixture } from "../test/today";
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

// line is the folded weather: the one button that names a temperature.
function line() {
  return screen.getByRole("button", { name: /°/ });
}

beforeEach(() => {
  for (const handler of Object.values(handlers)) handler.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubNarrow(narrow: boolean) {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: narrow,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
}

test("the first visit shows the weather and sun tiles", () => {
  show();
  const section = screen.getByRole("region", {
    name: "Today · Monday, November 9",
  });
  expect(
    within(section).getByRole("button", {
      name: "Zürich · from your memories",
    }),
  ).toBeInTheDocument();
  expect(within(section).queryByRole("button", { name: /°/ })).toBeNull();
  expect(within(section).getByText("10°")).toBeInTheDocument();
  expect(
    within(section).getByRole("img", { name: "Partly cloudy" }),
  ).toBeInTheDocument();
  expect(
    within(section).getByText("H 11° · L 3° · Rain 10%"),
  ).toBeInTheDocument();
  const hours = within(section).getByRole("list", { name: "Next hours" });
  expect(within(hours).getAllByRole("listitem")).toHaveLength(5);
  // 21:00 is after sunset: the clear sky is a moon.
  expect(within(hours).getAllByRole("listitem")[4]).toHaveAttribute(
    "title",
    "Clear",
  );
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
  const credit = within(section).getByRole("link", {
    name: "Weather data by Open-Meteo.com",
  });
  expect(credit).toHaveAttribute("href", "https://open-meteo.com/");
  expect(credit).toHaveAttribute("target", "_blank");
  expect(credit).toHaveAttribute("rel", "noopener");
});

test("a later visit folds the weather into a line that unfolds the tiles", () => {
  show({ view: "line" });
  const section = screen.getByRole("region", { name: "Today" });
  const line = within(section).getByRole("button", { name: /°/ });
  expect(line).toHaveAttribute("aria-expanded", "false");
  expect(line).toHaveTextContent("10° Partly cloudy");
  expect(line).toHaveTextContent("Sunset 16:58");
  expect(
    within(section).queryByRole("list", { name: "Next hours" }),
  ).toBeNull();
  expect(
    within(section).getByRole("link", {
      name: "Weather data by Open-Meteo.com",
    }),
  ).toBeInTheDocument();
  fireEvent.click(line);
  expect(line).toHaveAttribute("aria-expanded", "true");
  expect(
    within(section).getByRole("list", { name: "Next hours" }),
  ).toBeInTheDocument();
  fireEvent.click(line);
  expect(line).toHaveAttribute("aria-expanded", "false");
});

test("a phone starts with the line even on the first visit", () => {
  stubNarrow(true);
  show({ view: "tiles" });
  expect(line()).toHaveTextContent("10° Partly cloudy");
  expect(screen.queryByRole("list", { name: "Next hours" })).toBeNull();
});

test("a warning stands out in the line and in the tile", () => {
  show({ view: "line", warning: { kind: "rain", from: "17:00" } });
  const warning = screen.getByTestId("line-warning");
  expect(warning).toHaveTextContent("Rain from 17:00");
  expect(warning).toHaveClass("text-[#d97757]");
  fireEvent.click(line());
  expect(screen.getByTestId("weather-warning")).toHaveTextContent(
    "Rain from 17:00",
  );
  expect(screen.getByTestId("weather-warning")).toHaveClass("text-[#d97757]");
  // The rainy 18:00 slot is drawn in the accent, the sunny ones are not.
  const hours = within(
    screen.getByRole("list", { name: "Next hours" }),
  ).getAllByRole("listitem");
  expect(hours[3].querySelector("svg")).toHaveAttribute("stroke", "#d97757");
  expect(hours[1].querySelector("svg")).toHaveAttribute("stroke", "#e0b46a");
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
    view: "line",
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

test("the line names the sunrise before it, the polar day as such", () => {
  const sun = todayFixture().sun!;
  const { unmount } = show({
    view: "line",
    sun: { ...sun, progress: -0.2, deltaMinutes: 0 },
  });
  expect(line()).toHaveTextContent("Sunrise 07:25");
  fireEvent.click(line());
  expect(
    screen.getByText("Day length 9 h 33 min · same as yesterday"),
  ).toBeInTheDocument();
  unmount();
  show({
    view: "tiles",
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

test("after sunset the sun is set; the polar night has no sun", () => {
  const sun = todayFixture().sun!;
  const { unmount } = show({ sun: { ...sun, progress: 1.3 } });
  expect(
    screen.getByRole("img", { name: "Sun position: set" }),
  ).toBeInTheDocument();
  unmount();
  show({
    view: "line",
    sun: { ...sun, rise: null, set: null, polar: "night", dayMinutes: 0 },
  });
  expect(line()).toHaveTextContent("Polar night");
});

test("without weather the sun times show alone", () => {
  show({ weather: null, runs: [] });
  const group = screen.getByRole("group", { name: "Today" });
  expect(group).toHaveTextContent("Sunrise 07:25");
  expect(group).toHaveTextContent("Sunset 16:58");
  expect(screen.queryByRole("link")).toBeNull();
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
