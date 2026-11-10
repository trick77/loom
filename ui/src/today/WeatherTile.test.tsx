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
import { STEP_MS } from "./useTileClock";
import { WeatherTiles } from "./WeatherTile";

function show(today: Today, weather: Partial<TodayWeather> = {}) {
  return render(
    <WeatherTiles today={today} weather={{ ...today.weather!, ...weather }} />,
  );
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// left is the sky's course (now, rain, the days ahead), right the
// conditions (sun, details, air).
const left = () => screen.getByRole("region", { name: "Weather" });
const right = () => screen.getByRole("region", { name: "Conditions" });
const cardName = (tile = left()) => within(tile).getByTestId("card-name");
const track = (tile = left()) => within(tile).getByTestId("carousel-track");
const row = (tile = left()) => track(tile).firstElementChild as HTMLElement;
const first = (tile = left()) => row(tile).firstElementChild as HTMLElement;
const next = (tile = left()) =>
  fireEvent.click(within(tile).getByRole("button", { name: "Next card" }));
const open = (name: string) =>
  fireEvent.click(screen.getByRole("button", { name }));
const wait = (ms: number) => act(() => vi.advanceTimersByTime(ms));
// The card in view: the only slide not hidden.
const shown = (tile = left()) => {
  const slides = within(tile).getAllByRole("group");
  expect(slides).toHaveLength(1);
  return slides[0];
};

test("the tiles open on now and the sun, the other cards out of view", () => {
  show(todayFixture());
  expect(cardName()).toHaveTextContent("Now");
  expect(shown()).toHaveAccessibleName("Now, 1 of 3");
  expect(within(shown()).getByText("10°")).toBeInTheDocument();
  expect(
    within(left()).getByRole("button", { name: "Previous card" }),
  ).toBeDisabled();
  expect(screen.getByRole("button", { name: "Now" })).toHaveAttribute(
    "aria-current",
    "true",
  );
  expect(cardName(right())).toHaveTextContent("Sun");
  expect(shown(right())).toHaveAccessibleName("Sun, 1 of 3");
  expect(within(shown(right())).getByText("07:25")).toBeInTheDocument();
  // The rest are rendered beside it, inert: the tallest sets the height.
  for (const [tile, names] of [
    [left(), ["Now", "Rain", "Next days"]],
    [right(), ["Sun", "Details", "Air & pollen"]],
  ] as const) {
    const all = within(tile).getAllByRole("group", { hidden: true });
    expect(all.map((slide) => slide.getAttribute("aria-label"))).toEqual(
      names.map((name, i) => `${name}, ${i + 1} of 3`),
    );
    for (const slide of all.slice(1)) {
      expect(slide).toHaveAttribute("aria-hidden", "true");
      expect(slide).toHaveAttribute("inert");
    }
  }
});

test("a side without cards leaves one tile", () => {
  show(todayFixture({ sun: null, air: null }), { details: null });
  expect(screen.getAllByRole("region")).toHaveLength(1);
  expect(cardName()).toHaveTextContent("Now");
});

test("details: the wind leads, the rest in a strip", () => {
  show(todayFixture());
  next(right());
  expect(cardName(right())).toHaveTextContent("Details");
  const card = shown(right());
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
  next(right());
  const card = shown(right());
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
  next(right());
  expect(within(shown(right())).getByTestId("details-hero")).toHaveTextContent(
    "7°Feels like",
  );
  expect(within(shown(right())).queryByTitle("Feels like")).toBeNull();
  unmount();
  show(todayFixture(), {
    details: { ...details, wind: null, gusts: null, feelsLike: null },
  });
  next(right());
  expect(within(shown(right())).queryByTestId("details-hero")).toBeNull();
  expect(within(shown(right())).getByTitle("Humidity")).toBeInTheDocument();
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
  next(right());
  expect(cardName()).toHaveTextContent("Next days");
  expect(cardName(right())).toHaveTextContent("Details");
  const tomorrow = tomorrowFixture();
  rerender(<WeatherTiles today={tomorrow} weather={tomorrow.weather!} />);
  expect(cardName()).toHaveTextContent("Overview");
  expect(cardName(right())).toHaveTextContent("Sun");
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
  expect(cardName(right())).toHaveTextContent("Air & pollen");
  const card = shown(right());
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
  const card = shown(right());
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
  expect(within(shown(right())).getByTestId("air-quality")).toHaveTextContent(
    "64Air poor at worstaround 14:00",
  );
});

test("air where pollen is unknown shows no pollen at all; no hours, no curve", () => {
  const base = todayFixture();
  show(todayFixture({ air: { ...base.air!, pollen: null, hours: [] } }));
  open("Air & pollen");
  const card = shown(right());
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
  expect(within(shown(right())).getByTestId("air-quality")).toHaveTextContent(
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
  fireEvent.keyDown(left(), { key: "ArrowLeft" });
  expect(cardName()).toHaveTextContent("Now");
  fireEvent.keyDown(left(), { key: "ArrowRight" });
  expect(cardName()).toHaveTextContent("Rain");
  // A swipe to the left turns on, one to the right back.
  const swipe = (dx: number, dy = 0) => {
    fireEvent.pointerDown(track(), { clientX: 200, clientY: 100 });
    fireEvent.pointerUp(track(), { clientX: 200 + dx, clientY: 100 + dy });
  };
  swipe(-80);
  expect(cardName()).toHaveTextContent("Next days");
  swipe(80);
  expect(cardName()).toHaveTextContent("Rain");
  // Too short, or mostly up and down (a scroll): no turn.
  swipe(-20);
  swipe(-60, 90);
  expect(cardName()).toHaveTextContent("Rain");
  // A canceled pointer turns nothing.
  fireEvent.pointerDown(track(), { clientX: 200, clientY: 100 });
  fireEvent.pointerCancel(track());
  fireEvent.pointerUp(track(), { clientX: 50, clientY: 100 });
  expect(cardName()).toHaveTextContent("Rain");
});

test("a click on a tile turns it on, from the last on to the first", () => {
  show(todayFixture());
  fireEvent.click(track(right()));
  expect(cardName(right())).toHaveTextContent("Details");
  expect(cardName()).toHaveTextContent("Now");
  expect(row(right())).toHaveClass("transition-transform");
  fireEvent.click(track(right()));
  expect(cardName(right())).toHaveTextContent("Air & pollen");
  // On to the first, not back past every card: carried past the last, it
  // slides in from the right.
  fireEvent.click(track(right()));
  expect(cardName(right())).toHaveTextContent("Sun");
  expect(row(right())).toHaveClass("transition-transform");
  expect(row(right()).style.transform).toBe("translateX(-300%)");
  expect(first(right()).style.transform).toBe("translateX(300%)");
  // Once it stands, the row settles back at the start unseen.
  fireEvent.transitionEnd(row(right()));
  expect(row(right())).not.toHaveClass("transition-transform");
  expect(row(right()).style.transform).toBe("translateX(-0%)");
  expect(first(right()).style.transform).toBe("");
  expect(cardName(right())).toHaveTextContent("Sun");
  fireEvent.click(track(right()));
  expect(row(right())).toHaveClass("transition-transform");
  expect(row(right()).style.transform).toBe("translateX(-100%)");
  // The swipe's own click turns nothing more.
  fireEvent.pointerDown(track(), { clientX: 200, clientY: 100 });
  fireEvent.pointerUp(track(), { clientX: 120, clientY: 100 });
  fireEvent.click(track());
  expect(cardName()).toHaveTextContent("Rain");
});

test("a step back to the first card slides back; the wrap slides on", () => {
  // Two cards: back from the second is one step, not a wrap.
  show(todayFixture({ air: null }), { outlook: null });
  next();
  expect(cardName()).toHaveTextContent("Rain");
  fireEvent.click(
    within(left()).getByRole("button", { name: "Previous card" }),
  );
  expect(cardName()).toHaveTextContent("Now");
  expect(row()).toHaveClass("transition-transform");
  next();
  fireEvent.click(screen.getByRole("button", { name: "Now" }));
  expect(row()).toHaveClass("transition-transform");
  expect(row().style.transform).toBe("translateX(-0%)");
  expect(first().style.transform).toBe("");
  // A click on the track from the last is the wrap.
  next();
  fireEvent.click(track());
  expect(cardName()).toHaveTextContent("Now");
  expect(row()).toHaveClass("transition-transform");
  expect(row().style.transform).toBe("translateX(-200%)");
  expect(first().style.transform).toBe("translateX(200%)");
});

test("a turn while the wrap slides jumps; a card's own transition ends nothing", () => {
  show(todayFixture());
  next();
  next();
  fireEvent.click(track());
  expect(row().style.transform).toBe("translateX(-300%)");
  // A transition inside a card bubbles up: the row slides on.
  fireEvent.transitionEnd(within(first()).getByText("10°"));
  expect(row().style.transform).toBe("translateX(-300%)");
  // Turned on before it stands: sliding, it would run back past every card.
  next();
  expect(cardName()).toHaveTextContent("Rain");
  expect(row()).not.toHaveClass("transition-transform");
  expect(row().style.transform).toBe("translateX(-100%)");
  expect(first().style.transform).toBe("");
  next();
  expect(row()).toHaveClass("transition-transform");
});

test("a turn that moves nothing leaves the beat alone", () => {
  vi.useFakeTimers();
  show(todayFixture());
  wait(10_000);
  // Already on the first: the key, the dot and the swipe go nowhere.
  fireEvent.keyDown(left(), { key: "ArrowLeft" });
  fireEvent.click(screen.getByRole("button", { name: "Now" }));
  fireEvent.pointerDown(track(), { clientX: 100, clientY: 100 });
  fireEvent.pointerUp(track(), { clientX: 200, clientY: 100 });
  wait(5_000);
  expect(cardName()).toHaveTextContent("Rain");
  expect(cardName(right())).toHaveTextContent("Sun");
});

test("new cards from a refresh keep the beat", () => {
  vi.useFakeTimers();
  const base = todayFixture();
  const { rerender } = render(
    <WeatherTiles
      today={{ ...base, air: null }}
      weather={{ ...base.weather!, details: null }}
    />,
  );
  wait(10_000);
  rerender(<WeatherTiles today={base} weather={base.weather!} />);
  wait(5_000);
  expect(cardName()).toHaveTextContent("Rain");
});

test("the tiles turn in turn: left at 15 s, right at 30 s, each card 30 s", () => {
  vi.useFakeTimers();
  show(todayFixture());
  expect(STEP_MS).toBe(15_000);
  wait(14_999);
  expect(cardName()).toHaveTextContent("Now");
  wait(1);
  expect(cardName()).toHaveTextContent("Rain");
  expect(cardName(right())).toHaveTextContent("Sun");
  wait(15_000);
  expect(cardName()).toHaveTextContent("Rain");
  expect(cardName(right())).toHaveTextContent("Details");
  wait(15_000);
  expect(cardName()).toHaveTextContent("Next days");
  wait(15_000);
  expect(cardName(right())).toHaveTextContent("Air & pollen");
  // After the last the first, sliding on.
  wait(15_000);
  expect(cardName()).toHaveTextContent("Now");
  expect(row()).toHaveClass("transition-transform");
  expect(row().style.transform).toBe("translateX(-300%)");
});

test("a turn by hand starts the beat over: the other tile turns 15 s later", () => {
  vi.useFakeTimers();
  show(todayFixture());
  wait(10_000);
  fireEvent.click(track(right()));
  expect(cardName(right())).toHaveTextContent("Details");
  wait(14_999);
  expect(cardName()).toHaveTextContent("Now");
  wait(1);
  expect(cardName()).toHaveTextContent("Rain");
  expect(cardName(right())).toHaveTextContent("Details");
  wait(15_000);
  expect(cardName(right())).toHaveTextContent("Air & pollen");
});

test("the card's bar fills over its 30 s; the tile due next starts half full", () => {
  vi.useFakeTimers();
  show(todayFixture());
  const bar = (tile: HTMLElement) =>
    within(tile).getByTestId("card-progress").firstElementChild!;
  expect(bar(left()).getAttribute("style")).toContain(
    "30000ms linear -15000ms",
  );
  expect(bar(right()).getAttribute("style")).toContain("30000ms linear 0ms");
  wait(15_000);
  expect(bar(left()).getAttribute("style")).toContain("30000ms linear 0ms");
});

test("a lone turning tile turns every 30 s", () => {
  vi.useFakeTimers();
  // The right tile has the sun alone: nothing to turn there.
  show(todayFixture({ air: null }), { details: null });
  expect(within(right()).queryByTestId("card-name")).toBeNull();
  wait(15_000);
  expect(cardName()).toHaveTextContent("Rain");
  wait(15_000);
  expect(cardName()).toHaveTextContent("Rain");
  wait(15_000);
  expect(cardName()).toHaveTextContent("Next days");
});

test("a mouse over either tile or keyboard focus in it holds both", () => {
  vi.useFakeTimers();
  show(todayFixture());
  // Turning by itself, the card's name is not announced.
  expect(cardName()).toHaveAttribute("aria-live", "off");
  fireEvent.pointerEnter(right(), { pointerType: "mouse" });
  wait(60_000);
  expect(cardName()).toHaveTextContent("Now");
  expect(cardName(right())).toHaveTextContent("Sun");
  expect(cardName()).toHaveAttribute("aria-live", "polite");
  fireEvent.pointerLeave(right(), { pointerType: "mouse" });
  wait(15_000);
  expect(cardName()).toHaveTextContent("Rain");
  const button = within(left()).getByRole("button", { name: "Next card" });
  fireEvent.keyDown(document.body, { key: "Tab" });
  fireEvent.focus(button);
  wait(60_000);
  expect(cardName(right())).toHaveTextContent("Sun");
  fireEvent.blur(button);
  wait(15_000);
  expect(cardName(right())).toHaveTextContent("Details");
});

test("a tap or click holds nothing for good", () => {
  vi.useFakeTimers();
  show(todayFixture());
  // A tap: its emulated enter has no leave, and it focuses the button.
  fireEvent.pointerEnter(left(), { pointerType: "touch" });
  const button = within(left()).getByRole("button", { name: "Next card" });
  fireEvent.pointerDown(button, { pointerType: "touch" });
  fireEvent.focus(button);
  fireEvent.click(button);
  expect(cardName()).toHaveTextContent("Rain");
  wait(15_000);
  expect(cardName(right())).toHaveTextContent("Details");
  wait(15_000);
  expect(cardName()).toHaveTextContent("Next days");
});

test("a hidden page or reduced motion turns nothing", () => {
  vi.useFakeTimers();
  const { unmount } = show(todayFixture());
  vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  wait(60_000);
  expect(cardName()).toHaveTextContent("Now");
  expect(cardName(right())).toHaveTextContent("Sun");
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
  wait(60_000);
  expect(cardName()).toHaveTextContent("Now");
  expect(cardName(right())).toHaveTextContent("Sun");
});

test("without further cards there is nothing to turn", () => {
  show(todayFixture({ air: null, sun: null }), {
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
