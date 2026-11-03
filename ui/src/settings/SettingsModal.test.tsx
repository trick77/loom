import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import { SettingsModal } from "./SettingsModal";

describe("SettingsModal", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    // The Usage section opens first; keep it loading.
    vi.spyOn(api, "getUsage").mockReturnValue(new Promise(() => {}));
  });

  it("hides the Apple Music section for users without the feature", () => {
    const status = vi.spyOn(api, "getAppleMusicStatus");
    render(<SettingsModal onClose={() => {}} appleMusic={false} />);

    expect(screen.getByRole("button", { name: "Usage" })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Apple Music" }),
    ).not.toBeInTheDocument();
    expect(status).not.toHaveBeenCalled();
  });

  it("opens the Apple Music section for users with the feature", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({ state: "never" });
    render(<SettingsModal onClose={() => {}} appleMusic />);

    fireEvent.click(screen.getByRole("button", { name: "Apple Music" }));

    expect(await screen.findByText("Not connected yet.")).toBeInTheDocument();
  });

  it("marks Apple Music and Strava with their brand, hidden from readers", () => {
    render(<SettingsModal onClose={() => {}} appleMusic strava />);

    for (const [name, brand] of [
      ["Apple Music", "appleMusic"],
      ["Strava", "strava"],
    ]) {
      const mark = screen
        .getByRole("button", { name })
        .querySelector(`svg[data-brand="${brand}"]`);
      expect(mark).toHaveAttribute("aria-hidden", "true");
    }
  });
});
