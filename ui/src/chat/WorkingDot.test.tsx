import "@testing-library/jest-dom/vitest";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { WorkingDot } from "./WorkingDot";

describe("WorkingDot", () => {
  it("renders three orbiting dots, each in its own positioning slot", () => {
    const { container } = render(<WorkingDot />);
    const root = container.querySelector(".ui-working-dots");
    expect(root).not.toBeNull();
    const slots = root!.querySelectorAll(":scope > .ui-working-orbit");
    expect(slots).toHaveLength(3);
    slots.forEach((slot) =>
      expect(slot.querySelector(":scope > .ui-working-dot")).not.toBeNull(),
    );
  });

  it("stays a polite status region with a screen-reader label", () => {
    render(<WorkingDot />);
    const status = screen.getByRole("status");
    expect(status.querySelector(".sr-only")).not.toBeNull();
    expect(status.querySelectorAll('[aria-hidden="true"]')).toHaveLength(3);
  });
});
