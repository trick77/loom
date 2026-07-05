import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import "../i18n";
import { setLanguage } from "../i18n";
import { UserMenu } from "./UserMenu";

vi.mock("../api", () => ({ updateMe: vi.fn().mockResolvedValue({}) }));

afterEach(() => setLanguage("en"));

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

  it("expands an English/Deutsch picker and switches on selection", async () => {
    const { updateMe } = await import("../api");
    const onClose = vi.fn();
    render(<UserMenu onSettings={vi.fn()} onLogout={vi.fn()} onClose={onClose} />);

    // Collapsed by default.
    expect(screen.queryByRole("menuitemradio", { name: "Deutsch (Deutschland)" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitem", { name: "Language" }));
    expect(screen.getByRole("menuitemradio", { name: "English (US)" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitemradio", { name: "Deutsch (Deutschland)" }));
    expect(updateMe).toHaveBeenCalledWith({ responseLanguage: "de" });
    expect(onClose).toHaveBeenCalled();
  });

  it("aligns icons with the first line of wrapping action text", () => {
    render(<UserMenu onSettings={vi.fn()} onLogout={vi.fn()} onClose={() => {}} />);

    const item = screen.getByRole("menuitem", { name: "Settings" });
    const icon = item.querySelector("[aria-hidden='true']");

    expect(item).toHaveClass("min-h-[30px]");
    expect(item).toHaveClass("items-start");
    expect(item).not.toHaveClass("items-center");
    expect(icon).toHaveClass("h-[21px]");
  });
});
