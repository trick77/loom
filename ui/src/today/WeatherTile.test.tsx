import "@testing-library/jest-dom/vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import type { Today, TodayWeather } from "../api";
import { todayFixture, tomorrowFixture } from "../test/today";
import {
  airCurve,
  compassKey,
  tempColor,
  untilMidnight,
  uvLevelKey,
} from "./WeatherCards";
import { AUTO_ADVANCE_MS, WeatherTile } from "./WeatherTile";

function show(today: Today, weather: Partial<TodayWeather> = {}) {
  return render(
    <WeatherTile today={today} weather={{ ...today.weather!, ...weather }} />,
  );
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const carousel = () => screen.getByRole("region", { name: "Weather" });
const cardName = () => screen.getByTestId("card-name");
const next = () =>
  fireEvent.click(screen.getByRole("button", { name: "Next card" }));
const open = (name: string) =>
  fireEvent.click(screen.getByRole("button", { name }));
// The card in view: the only slide not hidden.
const shown = () => {
  const slides = within(carousel()).getAllByRole("group");
  expect(slides).toHaveLength(1);
  return slides[0];
};

test("the tile opens on now, the other cards out of view", () => {
  show(todayFixture());
  expect(cardName()).toHaveTextContent("Now");
  expect(shown()).toHaveAccessibleName("Now, 1 of 5");
  expect(within(shown()).getByText("10°")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Previous card" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Now" })).toHaveAttribute(
    "aria-current",
    "true",
  );
  // The rest are rendered beside it, inert: the tallest sets the height.
  const hidden = within(carousel()).getAllByRole("group", { hidden: true });
  expect(hidden).toHaveLength(5);
  for (const slide of hidden.slice(1)) {
    expect(slide).toHaveAttribute("aria-hidden", "true");
    expect(slide).toHaveAttribute("inert");
  }
});

test("details: the wind leads, the rest in a strip", () => {
  show(todayFixture());
  next();
  expect(cardName()).toHaveTextContent("Details");
  const card = shown();
  const hero = within(card).getByTestId("details-hero");
  expect(hero).toHaveTextContent("12km/h");
  expect(hero).toHaveTextContent("Wind from the southwest");
  expect(hero).toHaveTextContent("Gusts up to 28 km/h");
  // The dial's arrow points where the wind blows to; the line says it, the
  // dial is not read twice.
  expect(within(card).queryByRole("img", { name: /Wind/ })).toBeNull();
  expect(within(card).getByTestId("wind-arrow")).toHaveAttribute(
    "transform",
    "rotate(50 20 20)",
  );
  for (const [label, value] of [
    ["Feels like", "7°"],
    ["Humidity", "81%"],
    ["UV index", "2"],
    ["Precipitation", "2.1 mm"],
    ["Sunshine", "3.5 h"],
  ]) {
    const item = within(card).getByTitle(label);
    expect(within(item).getByRole("definition")).toHaveTextContent(value);
  }
  expect(within(card).getByTitle("UV index")).toHaveTextContent("UV low");
});

test("tomorrow's details are its day's: the strongest wind, no air now", () => {
  show(tomorrowFixture());
  expect(cardName()).toHaveTextContent("Overview");
  next();
  const card = shown();
  expect(within(card).queryByTitle("Feels like")).toBeNull();
  expect(within(card).queryByTitle("Humidity")).toBeNull();
  const hero = within(card).getByTestId("details-hero");
  expect(hero).toHaveTextContent("30km/h");
  expect(hero).toHaveTextContent("Strongest wind, from the west");
});

test("details without wind lead with how it feels; without either, a strip", () => {
  const details = todayFixture().weather!.details!;
  const { unmount } = show(todayFixture(), {
    details: { ...details, wind: null, gusts: null },
  });
  next();
  expect(within(shown()).getByTestId("details-hero")).toHaveTextContent(
    "7°Feels like",
  );
  expect(within(shown()).queryByTitle("Feels like")).toBeNull();
  unmount();
  show(todayFixture(), {
    details: { ...details, wind: null, gusts: null, feelsLike: null },
  });
  next();
  expect(within(shown()).queryByTestId("details-hero")).toBeNull();
  expect(within(shown()).getByTitle("Humidity")).toBeInTheDocument();
});

test("rain: the amount leads, bars hour by hour", () => {
  show(todayFixture());
  open("Rain");
  expect(cardName()).toHaveTextContent("Rain");
  const card = shown();
  expect(within(card).getByTestId("rain-summary")).toHaveTextContent(
    "2.1mmup to 70% likelyheaviest at 19:00",
  );
  const hours = within(
    within(card).getByRole("list", { name: "Rain by hour" }),
  ).getAllByRole("listitem");
  expect(hours).toHaveLength(12);
  expect(hours[9]).toHaveAccessibleName("17:00 · 0.4 mm · 70%");
});

test("rain: dry needs no amount and a low chance; each says what it has", () => {
  const summary = (
    rain: TodayWeather["rain"],
    outlook?: TodayWeather["outlook"],
  ) => {
    const { unmount } = show(todayFixture(), {
      details: null,
      rain,
      ...(outlook === undefined ? {} : { outlook }),
    });
    next();
    const text = screen.getByTestId("rain-summary").textContent;
    unmount();
    return text;
  };
  const hours = todayFixture().weather!.rain!;
  // No amount, unlikely: dry, with the next rainy day from the outlook.
  const dry = hours.map((h) => ({ ...h, precipitation: 0, probability: 5 }));
  expect(summary(dry)).toBe("DryNo rain expectednext rain Wed 80%");
  expect(summary(dry, null)).toBe("DryNo rain expected");
  // No amount but likely (another model said so): never "no rain".
  expect(summary(hours.map((h) => ({ ...h, precipitation: 0 })))).toBe(
    "70%chance of rainlikeliest at 17:00",
  );
  // Chances alone.
  expect(
    summary([
      { time: "08:00", precipitation: null, probability: 10 },
      { time: "09:00", precipitation: null, probability: 40 },
    ]),
  ).toBe("40%chance of rainlikeliest at 09:00");
  // Amounts without chances: no "0% likely".
  expect(summary(hours.map((h) => ({ ...h, probability: null })))).toBe(
    "2.1mmin allheaviest at 19:00",
  );
});

test("a dry card shows the days ahead instead of empty hours", () => {
  const dry = todayFixture().weather!.rain!.map((h) => ({
    ...h,
    precipitation: 0,
    probability: 5,
  }));
  const { unmount } = show(todayFixture(), { details: null, rain: dry });
  next();
  const card = shown();
  expect(within(card).queryByRole("list", { name: "Rain by hour" })).toBeNull();
  const days = within(
    within(card).getByRole("list", { name: "Chance of rain by day" }),
  ).getAllByRole("listitem");
  expect(days).toHaveLength(5);
  expect(days[1]).toHaveAccessibleName("Wed · 80%");
  expect(days[1]).toHaveTextContent("80%");
  // An unlikely day says no number.
  expect(days[4]).not.toHaveTextContent("%");
  unmount();
  // Without an outlook the hours stay, as a base line.
  show(todayFixture(), { details: null, rain: dry, outlook: null });
  next();
  expect(
    within(shown()).getByRole("list", { name: "Rain by hour" }),
  ).toBeInTheDocument();
});

test("a new shown day starts over on the first card", () => {
  const { rerender } = show(todayFixture());
  next();
  next();
  expect(cardName()).toHaveTextContent("Rain");
  const tomorrow = tomorrowFixture();
  rerender(<WeatherTile today={tomorrow} weather={tomorrow.weather!} />);
  expect(cardName()).toHaveTextContent("Overview");
});

test("outlook: the days after the shown one, as columns", () => {
  show(todayFixture());
  open("Next days");
  expect(cardName()).toHaveTextContent("Next days");
  const days = within(
    within(shown()).getByRole("list", { name: "Next days" }),
  ).getAllByRole("listitem");
  expect(days).toHaveLength(5);
  expect(days[0]).toHaveTextContent("Tue");
  expect(days[0]).toHaveTextContent("40%");
  expect(days[0]).toHaveTextContent("12°");
  expect(days[0]).toHaveTextContent("4°");
  expect(
    within(days[2]).getByRole("img", { name: "Snow" }),
  ).toBeInTheDocument();
  // An unlikely rain is left out.
  expect(days[3]).not.toHaveTextContent("%");
  // The range sits on the scale shared by all days (-1° to 12°).
  const range = within(days[0]).getByTestId("day-range-bar");
  expect(range.style.top).toBe("0%");
  expect(range.style.bottom).toBe(`${(5 / 13) * 100}%`);
});

test("air: the index leads, its curve, the pollen by load class", () => {
  show(todayFixture());
  open("Air & pollen");
  expect(cardName()).toHaveTextContent("Air & pollen");
  const card = shown();
  expect(within(card).getByTestId("air-quality")).toHaveTextContent(
    "18Air goodup to 31 at 16:00",
  );
  expect(
    within(card).getByRole("img", { name: "Air quality by hour" }),
  ).toBeInTheDocument();
  const pollen = within(
    within(card).getByRole("list", { name: "Pollen" }),
  ).getAllByRole("listitem");
  expect(pollen.map((li) => li.textContent)).toEqual([
    "Birch moderate",
    "Grass high",
  ]);
});

test("tomorrow's air is its worst; no pollen says so", () => {
  show(tomorrowFixture());
  open("Air & pollen");
  const card = shown();
  expect(within(card).getByTestId("air-quality")).toHaveTextContent(
    "64Air poor at worst",
  );
  expect(within(card).getByText("Hardly any pollen")).toBeInTheDocument();
  // The worst hour is named when the curve has it.
  cleanup();
  const base = tomorrowFixture();
  show(
    tomorrowFixture({
      air: {
        ...base.air!,
        hours: [
          { time: "06:00", aqi: 40 },
          { time: "14:00", aqi: 64 },
        ],
      },
    }),
  );
  open("Air & pollen");
  expect(within(shown()).getByTestId("air-quality")).toHaveTextContent(
    "64Air poor at worstaround 14:00",
  );
});

test("air where pollen is unknown shows no pollen at all; no hours, no curve", () => {
  const base = todayFixture();
  show(todayFixture({ air: { ...base.air!, pollen: null, hours: [] } }));
  open("Air & pollen");
  const card = shown();
  expect(within(card).getByTestId("air-quality")).toHaveTextContent(
    "18Air good",
  );
  expect(within(card).queryByText("Hardly any pollen")).toBeNull();
  expect(within(card).queryByRole("list", { name: "Pollen" })).toBeNull();
  expect(
    within(card).queryByRole("img", { name: "Air quality by hour" }),
  ).toBeNull();
});

test("today's air names today's peak, not tomorrow morning's", () => {
  const base = todayFixture();
  show(
    todayFixture({
      air: {
        ...base.air!,
        hours: [
          { time: "22:00", aqi: 20 },
          { time: "23:00", aqi: 24 },
          { time: "00:00", aqi: 50 },
          { time: "08:00", aqi: 60 },
        ],
      },
    }),
  );
  open("Air & pollen");
  expect(within(shown()).getByTestId("air-quality")).toHaveTextContent(
    "18Air goodup to 24 at 23:00",
  );
  expect(untilMidnight([{ time: "09:00", aqi: 1 }])).toHaveLength(1);
});

test("the air's curve: gaps break it, bands bound its height", () => {
  const curve = airCurve(todayFixture().air!.hours, 264, 38);
  // 20:00 has no index: two runs.
  expect(curve.runs).toHaveLength(2);
  expect(curve.runs[0]).toHaveLength(12);
  expect(curve.runs[1]).toHaveLength(11);
  // The peak (31) is fair: the scale runs to its band's top, 40, with the
  // good/fair line at 20.
  expect(curve.top).toBe(40);
  expect(curve.lines).toEqual([20]);
  expect(curve.peak).toEqual({ time: "16:00", aqi: 31 });
  expect(airCurve([{ time: "08:00", aqi: 140 }], 264, 38).top).toBe(140);
});

test("arrow keys and swipes turn the cards, within their ends", () => {
  show(todayFixture());
  const track = screen.getByTestId("carousel-track");
  fireEvent.keyDown(carousel(), { key: "ArrowLeft" });
  expect(cardName()).toHaveTextContent("Now");
  fireEvent.keyDown(carousel(), { key: "ArrowRight" });
  expect(cardName()).toHaveTextContent("Details");
  // A swipe to the left turns on, one to the right back.
  const swipe = (dx: number, dy = 0) => {
    fireEvent.pointerDown(track, { clientX: 200, clientY: 100 });
    fireEvent.pointerUp(track, { clientX: 200 + dx, clientY: 100 + dy });
  };
  swipe(-80);
  expect(cardName()).toHaveTextContent("Rain");
  swipe(80);
  expect(cardName()).toHaveTextContent("Details");
  // Too short, or mostly up and down (a scroll): no turn.
  swipe(-20);
  swipe(-60, 90);
  expect(cardName()).toHaveTextContent("Details");
  // A canceled pointer turns nothing.
  fireEvent.pointerDown(track, { clientX: 200, clientY: 100 });
  fireEvent.pointerCancel(track);
  fireEvent.pointerUp(track, { clientX: 50, clientY: 100 });
  expect(cardName()).toHaveTextContent("Details");
});

test("the cards turn by themselves every 15 s and wrap around", () => {
  vi.useFakeTimers();
  show(todayFixture());
  expect(AUTO_ADVANCE_MS).toBe(15_000);
  act(() => vi.advanceTimersByTime(14_999));
  expect(cardName()).toHaveTextContent("Now");
  act(() => vi.advanceTimersByTime(1));
  expect(cardName()).toHaveTextContent("Details");
  open("Air & pollen");
  // A turn by hand starts the 15 s over.
  act(() => vi.advanceTimersByTime(10_000));
  expect(cardName()).toHaveTextContent("Air & pollen");
  act(() => vi.advanceTimersByTime(5_000));
  expect(cardName()).toHaveTextContent("Now");
});

test("a mouse over the tile or keyboard focus in it holds the card", () => {
  vi.useFakeTimers();
  show(todayFixture());
  // Turning by itself, the card's name is not announced.
  expect(cardName()).toHaveAttribute("aria-live", "off");
  fireEvent.pointerEnter(carousel(), { pointerType: "mouse" });
  act(() => vi.advanceTimersByTime(30_000));
  expect(cardName()).toHaveTextContent("Now");
  expect(cardName()).toHaveAttribute("aria-live", "polite");
  fireEvent.pointerLeave(carousel(), { pointerType: "mouse" });
  act(() => vi.advanceTimersByTime(15_000));
  expect(cardName()).toHaveTextContent("Details");
  const button = screen.getByRole("button", { name: "Next card" });
  fireEvent.keyDown(document.body, { key: "Tab" });
  fireEvent.focus(button);
  act(() => vi.advanceTimersByTime(30_000));
  expect(cardName()).toHaveTextContent("Details");
  fireEvent.blur(button);
  act(() => vi.advanceTimersByTime(15_000));
  expect(cardName()).toHaveTextContent("Rain");
});

test("a tap or click holds nothing for good", () => {
  vi.useFakeTimers();
  show(todayFixture());
  // A tap: its emulated enter has no leave, and it focuses the button.
  fireEvent.pointerEnter(carousel(), { pointerType: "touch" });
  const button = screen.getByRole("button", { name: "Next card" });
  fireEvent.pointerDown(button, { pointerType: "touch" });
  fireEvent.focus(button);
  fireEvent.click(button);
  expect(cardName()).toHaveTextContent("Details");
  act(() => vi.advanceTimersByTime(15_000));
  expect(cardName()).toHaveTextContent("Rain");
});

test("a hidden page or reduced motion turns nothing", () => {
  vi.useFakeTimers();
  const { unmount } = show(todayFixture());
  vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  act(() => vi.advanceTimersByTime(30_000));
  expect(cardName()).toHaveTextContent("Now");
  vi.restoreAllMocks();
  unmount();
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: query.includes("reduce"),
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  show(todayFixture());
  act(() => vi.advanceTimersByTime(30_000));
  expect(cardName()).toHaveTextContent("Now");
});

test("without further cards there is nothing to turn", () => {
  show(todayFixture({ air: null }), {
    details: null,
    rain: null,
    outlook: null,
  });
  expect(screen.queryByRole("button", { name: "Next card" })).toBeNull();
  expect(screen.queryByTestId("card-name")).toBeNull();
  expect(within(shown()).getByText("10°")).toBeInTheDocument();
});

test("a wind's direction, the UV's band and a temperature's color", () => {
  expect(compassKey(350)).toBe("n");
  expect(compassKey(230)).toBe("sw");
  expect(compassKey(-90)).toBe("w");
  expect(compassKey(720)).toBe("n");
  expect(uvLevelKey(2.4)).toBe("low");
  expect(uvLevelKey(2.6)).toBe("moderate");
  expect(uvLevelKey(7)).toBe("high");
  expect(uvLevelKey(9)).toBe("veryHigh");
  expect(uvLevelKey(11)).toBe("extreme");
  expect(tempColor(-20)).toBe(
    "color-mix(in oklab, var(--color-sun) 0%, var(--color-cold))",
  );
  expect(tempColor(10)).toBe(
    "color-mix(in oklab, var(--color-sun) 50%, var(--color-cold))",
  );
  expect(tempColor(40)).toBe(
    "color-mix(in oklab, var(--color-sun) 100%, var(--color-cold))",
  );
});
