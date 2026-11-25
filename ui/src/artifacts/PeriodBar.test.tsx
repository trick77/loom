import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { PeriodBar } from "./PeriodBar";

const october = { kind: "month", year: 2025, month: 10 } as const;

// A type switch or the delete of its last artifact can drop the listed month
// from the summary; the picker must still name it, not fall back to another.
test("the picked month stays shown when the summary no longer holds it", () => {
  const { rerender } = render(
    <PeriodBar
      period={october}
      months={[
        { month: "2025-12", count: 2 },
        { month: "2025-10", count: 1 },
      ]}
      searching={false}
      onChange={vi.fn()}
    />,
  );
  const picker = screen.getByRole("combobox", { name: "Pick month" });
  expect(picker).toHaveValue("2025-10");

  rerender(
    <PeriodBar
      period={october}
      months={[{ month: "2025-12", count: 2 }]}
      searching={false}
      onChange={vi.fn()}
    />,
  );
  expect(picker).toHaveValue("2025-10");
  expect(
    within(picker).getByRole("option", { name: "October 2025 (0)" }),
  ).toBeInTheDocument();

  // No months at all: the picker stays for the month being listed.
  rerender(
    <PeriodBar
      period={october}
      months={[]}
      searching={false}
      onChange={vi.fn()}
    />,
  );
  expect(screen.getByRole("combobox", { name: "Pick month" })).toHaveValue(
    "2025-10",
  );
});

// A type switch can leave the listed year without artifacts of that type; its
// chip must stay, pressed and in order, or no chip says what is listed.
test("the picked year stays shown when the summary no longer holds it", () => {
  const props = { searching: false, onChange: vi.fn() };
  const year2025 = { kind: "year", year: 2025 } as const;
  const chips = () =>
    screen.getAllByRole("button").map((button) => button.textContent);
  const { rerender } = render(
    <PeriodBar
      period={year2025}
      months={[
        { month: "2026-11", count: 1 },
        { month: "2024-03", count: 2 },
      ]}
      {...props}
    />,
  );
  expect(chips()).toEqual(["Last 30 days", "2026", "2025", "2024"]);
  expect(screen.getByRole("button", { name: "2025" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );

  // No months at all: the year still stands.
  rerender(<PeriodBar period={year2025} months={[]} {...props} />);
  expect(chips()).toEqual(["Last 30 days", "2025"]);
  expect(screen.getByRole("button", { name: "2025" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );

  // A search lists all time: the year stays (clearing the search returns to
  // it) but is not the one listed.
  rerender(
    <PeriodBar period={year2025} months={[]} searching onChange={vi.fn()} />,
  );
  expect(screen.getByRole("button", { name: "2025" })).toHaveAttribute(
    "aria-pressed",
    "false",
  );
});

test("no extra month while browsing a year or the recent window", () => {
  render(
    <PeriodBar
      period={{ kind: "year", year: 2025 }}
      months={[{ month: "2025-12", count: 2 }]}
      searching={false}
      onChange={vi.fn()}
    />,
  );
  const picker = screen.getByRole("combobox", { name: "Pick month" });
  expect(picker).toHaveValue("");
  // The placeholder and December.
  expect(within(picker).getAllByRole("option")).toHaveLength(2);
});
