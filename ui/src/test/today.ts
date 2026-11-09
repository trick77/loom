import type { Today } from "../api";

// tomorrowFixture is the same place shown from 18:00: tomorrow's range and
// daytime hours, no "now".
export function tomorrowFixture(overrides: Partial<Today> = {}): Today {
  const base = todayFixture();
  return todayFixture({
    date: "2026-11-10",
    day: "tomorrow",
    sun: { ...base.sun!, deltaMinutes: -2, progress: null },
    weather: {
      ...base.weather!,
      now: null,
      day: { high: 12, low: 4, code: 3, precipitationProbability: 40 },
      hours: [
        { time: "06:00", temperature: 4, code: 3, precipitationProbability: 5 },
        {
          time: "09:00",
          temperature: 7,
          code: 61,
          precipitationProbability: 60,
        },
        {
          time: "12:00",
          temperature: 11,
          code: 3,
          precipitationProbability: 20,
        },
        {
          time: "15:00",
          temperature: 12,
          code: 0,
          precipitationProbability: 0,
        },
        { time: "18:00", temperature: 9, code: 0, precipitationProbability: 0 },
      ],
    },
    ...overrides,
  });
}

// todayFixture is today in Zürich with two unread runs; tests take away what
// they do not need.
export function todayFixture(overrides: Partial<Today> = {}): Today {
  return {
    date: "2026-11-09",
    day: "today",
    location: { name: "Zürich", country: "Switzerland", kind: "home" },
    askLocation: false,
    sun: {
      rise: "07:25",
      set: "16:58",
      dayMinutes: 573,
      deltaMinutes: -2,
      polar: "",
      progress: 0.3,
    },
    weather: {
      now: { temperature: 9.6, code: 2 },
      day: { high: 11, low: 3, code: 2, precipitationProbability: 10 },
      hours: [
        { time: "09:00", temperature: 6, code: 3, precipitationProbability: 5 },
        { time: "12:00", temperature: 9, code: 0, precipitationProbability: 0 },
        {
          time: "15:00",
          temperature: 11,
          code: 0,
          precipitationProbability: 0,
        },
        {
          time: "18:00",
          temperature: 8,
          code: 61,
          precipitationProbability: 70,
        },
        {
          time: "21:00",
          temperature: 5,
          code: 0,
          precipitationProbability: null,
        },
      ],
      attribution: {
        text: "Weather data by Open-Meteo.com",
        url: "https://open-meteo.com/",
      },
    },
    warning: null,
    refreshAt: null,
    runs: [
      {
        id: "n1",
        threadId: "th1",
        title: "Tech briefing",
        body: "Three new articles, one security fix in net/http.",
        createdAt: new Date().toISOString(),
      },
      {
        id: "n2",
        threadId: null,
        title: "Flats in Zürich",
        body: "Two new listings under your limit.",
        createdAt: new Date().toISOString(),
      },
    ],
    ...overrides,
  };
}
