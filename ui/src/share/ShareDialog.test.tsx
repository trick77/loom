import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import { ShareDialog } from "./ShareDialog";

const sharedInfo: api.ShareInfo = {
  shareId: "tok123",
  shareUrl: "/share/tok123",
  shared: true,
  snapshotAt: "2026-06-28T00:00:00Z",
};

function renderDialog(overrides: Partial<Parameters<typeof ShareDialog>[0]> = {}) {
  const props = {
    threadId: "th1",
    share: null,
    hasNewMessages: false,
    onShareChange: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  };
  render(<ShareDialog {...props} />);
  return props;
}

describe("ShareDialog", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("renders the un-shared state with the create button disabled until 'public' is chosen", () => {
    renderDialog();

    expect(screen.getByRole("heading", { name: "Share chat" })).toBeInTheDocument();
    expect(screen.getByText("Only messages up to this point will be shared.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Create share link/ })).toBeDisabled();
    // No link row while the thread is private.
    expect(screen.queryByRole("button", { name: "Copy link" })).not.toBeInTheDocument();
  });

  it("creates a share when 'Create public link' is chosen and reports it upward", async () => {
    const createShare = vi.spyOn(api, "createShare").mockResolvedValue(sharedInfo);
    const { onShareChange } = renderDialog();

    fireEvent.click(screen.getByText("Create public link"));

    expect(createShare).toHaveBeenCalledWith("th1");
    await vi.waitFor(() => expect(onShareChange).toHaveBeenCalledWith(sharedInfo));
  });

  it("shows an error when creating the share fails", async () => {
    vi.spyOn(api, "createShare").mockRejectedValue(new Error("boom"));
    const { onShareChange } = renderDialog();

    fireEvent.click(screen.getByText("Create public link"));

    expect(await screen.findByText("Something went wrong. Please try again.")).toBeInTheDocument();
    expect(onShareChange).not.toHaveBeenCalled();
  });

  it("disables an active share when 'Keep private' is chosen", async () => {
    const disableShare = vi.spyOn(api, "disableShare").mockResolvedValue(undefined);
    const { onShareChange } = renderDialog({ share: sharedInfo });

    expect(screen.getByRole("heading", { name: "Chat shared" })).toBeInTheDocument();
    fireEvent.click(screen.getByText("Keep private"));

    expect(disableShare).toHaveBeenCalledWith("th1");
    await vi.waitFor(() =>
      expect(onShareChange).toHaveBeenCalledWith({ ...sharedInfo, shared: false }),
    );
  });

  it("offers an Update affordance only when the thread has newer messages", async () => {
    const refreshed: api.ShareInfo = { ...sharedInfo, snapshotAt: "2026-06-29T00:00:00Z" };
    const updateShare = vi.spyOn(api, "updateShare").mockResolvedValue(refreshed);

    const { rerender } = render(
      <ShareDialog
        threadId="th1"
        share={sharedInfo}
        hasNewMessages={false}
        onShareChange={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText("Future messages aren’t included.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Update" })).not.toBeInTheDocument();

    const onShareChange = vi.fn();
    rerender(
      <ShareDialog
        threadId="th1"
        share={sharedInfo}
        hasNewMessages
        onShareChange={onShareChange}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Update" }));

    expect(updateShare).toHaveBeenCalledWith("th1");
    await vi.waitFor(() => expect(onShareChange).toHaveBeenCalledWith(refreshed));
  });

  it("copies the absolute share URL to the clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderDialog({ share: sharedInfo });

    const expected = `${window.location.origin}/share/tok123`;
    expect(screen.getByTitle(expected)).toHaveTextContent(expected);

    fireEvent.click(screen.getByRole("button", { name: "Copy link" }));

    expect(writeText).toHaveBeenCalledWith(expected);
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("keeps an already-absolute share URL untouched and reports copy failures", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    Object.assign(navigator, { clipboard: { writeText } });
    renderDialog({ share: { ...sharedInfo, shareUrl: "https://loom.example/share/tok123" } });

    fireEvent.click(screen.getByRole("button", { name: "Copy link" }));

    expect(writeText).toHaveBeenCalledWith("https://loom.example/share/tok123");
    expect(await screen.findByText("Couldn’t copy the link.")).toBeInTheDocument();
  });

  it("closes on Escape, on the close button, and on a backdrop click", () => {
    const { onClose } = renderDialog();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(2);

    // Only a mousedown on the backdrop itself dismisses; clicks inside do not.
    fireEvent.mouseDown(screen.getByRole("dialog").firstChild as Element);
    expect(onClose).toHaveBeenCalledTimes(2);
    fireEvent.mouseDown(screen.getByRole("dialog"));
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});
