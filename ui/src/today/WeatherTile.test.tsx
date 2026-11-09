import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test } from "vitest";

import type { Today, TodayWeather } from "../api";
import { todayFixture, tomorrowFixture } from "../test/today";
import { compassKey, uvLevelKey } from "./WeatherCards";
import { WeatherTile } from "./WeatherTile";

function show(today: Today, weather: Partial<TodayWeather> = {}) {
  return render(
    <WeatherTile today={today} weather={{ ...today.weather!, ...weather }} />,
  );
}

const carousel = () => screen.getByRole("region", { name: "Weather" });
const cardName = () => screen.getByTestId("card-name");
const next = () =>
  fireEvent.click(screen.getByRole("button", { name: "Next card" }));
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

test("details: the air and wind now, the day's UV, rain and sun", () => {
  show(todayFixture());
  next();
  expect(cardName()).toHaveTextContent("Details");
  const card = shown();
  for (const [label, value] of [
    ["Feels like", "7°"],
    ["Humidity", "81%"],
    ["Wind", "12 km/h SW"],
    ["Gusts", "28 km/h"],
    ["UV index", "2 · low"],
    ["Precipitation", "2.1 mm"],
    ["Sunshine", "3.5 h"],
  ]) {
    expect(within(card).getByText(label).nextElementSibling).toHaveTextContent(
      value,
    );
  }
});

test("tomorrow's details are its day's: the strongest wind, no air now", () => {
  show(tomorrowFixture());
  expect(cardName()).toHaveTextContent("Overview");
  next();
  const card = shown();
  expect(within(card).queryByText("Feels like")).toBeNull();
  expect(within(card).queryByText("Humidity")).toBeNull();
  expect(
    within(card).getByText("Wind up to").nextElementSibling,
  ).toHaveTextContent("30 km/h W");
});

test("rain: hour by hour, with the total and how likely", () => {
  show(todayFixture());
  next();
  next();
  expect(cardName()).toHaveTextContent("Rain");
  const card = shown();
  expect(within(card).getByTestId("rain-summary")).toHaveTextContent(
    "2.1 mm in all, up to 70% likely",
  );
  const hours = within(
    within(card).getByRole("list", { name: "Rain by hour" }),
  ).getAllByRole("listitem");
  expect(hours).toHaveLength(12);
  expect(hours[9]).toHaveAccessibleName("17:00 · 0.4 mm · 70%");
});

test("rain: a dry stretch says so, chances alone draw by chance", () => {
  const dry = todayFixture().weather!.rain!.map((hour) => ({
    ...hour,
    precipitation: 0,
  }));
  const { unmount } = show(todayFixture(), { details: null, rain: dry });
  next();
  expect(screen.getByTestId("rain-summary")).toHaveTextContent(
    "No rain expected",
  );
  unmount();
  show(todayFixture(), {
    details: null,
    rain: [
      { time: "08:00", precipitation: null, probability: 10 },
      { time: "09:00", precipitation: null, probability: 40 },
    ],
  });
  next();
  expect(screen.getByTestId("rain-summary")).toHaveTextContent(
    "Rain up to 40% likely",
  );
});

test("outlook: the days after the shown one", () => {
  show(todayFixture());
  fireEvent.click(screen.getByRole("button", { name: "Next days" }));
  expect(cardName()).toHaveTextContent("Next days");
  const days = within(
    within(shown()).getByRole("list", { name: "Next days" }),
  ).getAllByRole("listitem");
  expect(days).toHaveLength(5);
  expect(days[0]).toHaveTextContent("Tue");
  expect(days[0]).toHaveTextContent("40%");
  expect(days[0]).toHaveTextContent("4°");
  expect(days[0]).toHaveTextContent("12°");
  expect(
    within(days[2]).getByRole("img", { name: "Snow" }),
  ).toBeInTheDocument();
  // An unlikely rain is left out.
  expect(days[3]).not.toHaveTextContent("%");
});

test("air: the index now with its band, the pollen by load class", () => {
  show(todayFixture());
  fireEvent.click(screen.getByRole("button", { name: "Air & pollen" }));
  expect(cardName()).toHaveTextContent("Air & pollen");
  const card = shown();
  expect(within(card).getByText("Air quality")).toBeInTheDocument();
  expect(within(card).getByTestId("air-quality")).toHaveTextContent(
    "18 · good",
  );
  const pollen = within(
    within(card).getByRole("list", { name: "Pollen" }),
  ).getAllByRole("listitem");
  expect(pollen.map((li) => li.textContent)).toEqual([
    "Birchmoderate",
    "Grasshigh",
  ]);
  // The last card: nothing further.
  expect(screen.getByRole("button", { name: "Next card" })).toBeDisabled();
});

test("tomorrow's air is its worst; no pollen says so", () => {
  show(tomorrowFixture());
  fireEvent.click(screen.getByRole("button", { name: "Air & pollen" }));
  const card = shown();
  expect(within(card).getByText("Air quality, worst")).toBeInTheDocument();
  expect(within(card).getByTestId("air-quality")).toHaveTextContent(
    "64 · poor",
  );
  expect(within(card).getByText("Hardly any pollen")).toBeInTheDocument();
});

test("air where pollen is unknown shows no pollen line at all", () => {
  const base = todayFixture();
  show(todayFixture({ air: { ...base.air!, pollen: null } }));
  fireEvent.click(screen.getByRole("button", { name: "Air & pollen" }));
  const card = shown();
  expect(within(card).getByTestId("air-quality")).toHaveTextContent(
    "18 · good",
  );
  expect(within(card).queryByText("Hardly any pollen")).toBeNull();
  expect(within(card).queryByRole("list", { name: "Pollen" })).toBeNull();
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

test("a wind's direction and the UV's band", () => {
  expect(compassKey(350)).toBe("n");
  expect(compassKey(230)).toBe("sw");
  expect(compassKey(-90)).toBe("w");
  expect(compassKey(720)).toBe("n");
  expect(uvLevelKey(2.4)).toBe("low");
  expect(uvLevelKey(2.6)).toBe("moderate");
  expect(uvLevelKey(7)).toBe("high");
  expect(uvLevelKey(9)).toBe("veryHigh");
  expect(uvLevelKey(11)).toBe("extreme");
});
