import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import { SharedChatsPanel } from "./SharedChatsPanel";

const active: api.ShareListItem = {
  shareId: "tok1",
  shareUrl: "/share/tok1",
  shared: true,
  snapshotAt: "2026-06-28T00:00:00Z",
  threadId: "th1",
  title: "Comparing gateways",
};

const disabled: api.ShareListItem = {
  shareId: "tok2",
  shareUrl: "https://loom.example/share/tok2",
  shared: false,
  snapshotAt: "2026-06-20T00:00:00Z",
  threadId: "th2",
  title: "",
};

describe("SharedChatsPanel", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("lists active and disabled shares with the right per-row controls", async () => {
    vi.spyOn(api, "getMyShares").mockResolvedValue([active, disabled]);
    render(<SharedChatsPanel />);

    expect(await screen.findByText("Comparing gateways")).toBeInTheDocument();
    // A share with no title falls back to the placeholder.
    expect(screen.getByText("Untitled")).toBeInTheDocument();
    expect(screen.getByText("· disabled")).toBeInTheDocument();
    // Only the active row is actionable.
    expect(screen.getAllByRole("button", { name: "Copy link" })).toHaveLength(1);
    expect(screen.getAllByRole("button", { name: "Disable link" })).toHaveLength(1);
    expect(screen.queryByText("You haven’t shared any chats yet.")).not.toBeInTheDocument();
  });

  it("shows an empty-state notice when nothing has been shared", async () => {
    vi.spyOn(api, "getMyShares").mockResolvedValue([]);
    render(<SharedChatsPanel />);

    expect(await screen.findByText("You haven’t shared any chats yet.")).toBeInTheDocument();
  });

  it("shows an error message when the list fails to load", async () => {
    vi.spyOn(api, "getMyShares").mockRejectedValue(new Error("boom"));
    render(<SharedChatsPanel />);

    expect(await screen.findByText("Failed to load shared chats.")).toBeInTheDocument();
  });

  it("revokes a share and flips the row to disabled", async () => {
    vi.spyOn(api, "getMyShares").mockResolvedValue([active]);
    const disableShare = vi.spyOn(api, "disableShare").mockResolvedValue(undefined);
    render(<SharedChatsPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Disable link" }));

    expect(disableShare).toHaveBeenCalledWith("th1");
    expect(await screen.findByText("· disabled")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Disable link" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy link" })).not.toBeInTheDocument();
  });

  it("surfaces an error when revoking fails and keeps the row active", async () => {
    vi.spyOn(api, "getMyShares").mockResolvedValue([active]);
    vi.spyOn(api, "disableShare").mockRejectedValue(new Error("boom"));
    render(<SharedChatsPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Disable link" }));

    expect(await screen.findByText("Failed to disable the link.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Disable link" })).toBeInTheDocument();
  });

  it("copies a relative share URL as an absolute one and confirms", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    vi.spyOn(api, "getMyShares").mockResolvedValue([active]);
    render(<SharedChatsPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Copy link" }));

    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/share/tok1`);
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("reports a clipboard failure instead of claiming success", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    Object.assign(navigator, { clipboard: { writeText } });
    vi.spyOn(api, "getMyShares").mockResolvedValue([active]);
    render(<SharedChatsPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Copy link" }));

    expect(await screen.findByText("Couldn’t copy the link.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copied" })).not.toBeInTheDocument();
  });
});
