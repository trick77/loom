import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { UserMenu } from "./UserMenu";

describe("UserMenu", () => {
  it("renders Settings, Language and Log out and fires callbacks", () => {
    const onSettings = vi.fn();
    const onLogout = vi.fn();
    render(<UserMenu onSettings={onSettings} onLogout={onLogout} onClose={() => {}} />);

    expect(screen.getByRole("menuitem", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Language" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitem", { name: "Settings" }));
    expect(onSettings).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("menuitem", { name: "Log out" }));
    expect(onLogout).toHaveBeenCalledOnce();
  });

  it("Language is inert (does not throw, no callbacks)", () => {
    const onSettings = vi.fn();
    const onLogout = vi.fn();
    render(<UserMenu onSettings={onSettings} onLogout={onLogout} onClose={() => {}} />);

    fireEvent.click(screen.getByRole("menuitem", { name: "Language" }));
    expect(onSettings).not.toHaveBeenCalled();
    expect(onLogout).not.toHaveBeenCalled();
  });
});
