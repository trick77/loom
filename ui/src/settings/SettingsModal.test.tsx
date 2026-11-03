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

  it("marks Apple Music and Strava with their brand, the rest from the icon font", () => {
    render(<SettingsModal onClose={() => {}} appleMusic strava />);

    for (const name of ["Apple Music", "Strava"]) {
      const button = screen.getByRole("button", { name });
      expect(button.querySelector("svg")).toHaveAttribute(
        "aria-hidden",
        "true",
      );
      expect(button.querySelector("span[aria-hidden]")).toBeNull();
    }
    for (const name of ["Usage", "Shared chats"]) {
      const button = screen.getByRole("button", { name });
      expect(button.querySelector("svg")).toBeNull();
      expect(button.querySelector("span[aria-hidden]")).not.toBeNull();
    }
  });
});
