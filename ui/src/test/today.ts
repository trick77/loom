import type { Today } from "../api";

// tomorrowFixture is the same place shown from 20:00: tomorrow's range and
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
      details: {
        feelsLike: null,
        humidity: null,
        wind: 30,
        gusts: 55,
        windDirection: 260,
        uvMax: 1.2,
        precipitationSum: 1.5,
        sunshineHours: 0.5,
      },
      outlook: base.weather!.outlook!.map((day, i) => ({
        ...day,
        date: `2026-11-${11 + i}`,
      })),
    },
    ...overrides,
  });
}

// rainHours is twelve hours from 08:00 with a shower from 17:00.
function rainHours() {
  return Array.from({ length: 12 }, (_, i) => {
    const wet = i >= 9;
    return {
      time: `${String(8 + i).padStart(2, "0")}:00`,
      precipitation: wet ? 0.4 + (i - 9) * 0.3 : 0,
      probability: wet ? 70 : 5,
    };
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
      details: {
        feelsLike: 7.4,
        humidity: 81,
        wind: 12,
        gusts: 28,
        windDirection: 230,
        uvMax: 1.6,
        precipitationSum: 2.1,
        sunshineHours: 3.5,
      },
      rain: rainHours(),
      outlook: [
        {
          date: "2026-11-10",
          high: 12,
          low: 4,
          code: 3,
          precipitationProbability: 40,
        },
        {
          date: "2026-11-11",
          high: 8,
          low: 1,
          code: 61,
          precipitationProbability: 80,
        },
        {
          date: "2026-11-12",
          high: 6,
          low: -1,
          code: 71,
          precipitationProbability: 60,
        },
        {
          date: "2026-11-13",
          high: 7,
          low: 0,
          code: 0,
          precipitationProbability: 0,
        },
        {
          date: "2026-11-14",
          high: 9,
          low: 2,
          code: 2,
          precipitationProbability: 5,
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
