import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import { AppleMusicPanel } from "./AppleMusicPanel";

const hoursAgo = (h: number) =>
  new Date(Date.now() - h * 3_600_000).toISOString();

const connected: api.AppleMusicStatus = {
  state: "connected",
  storefront: "ch",
  playlistCount: 12,
  lastOkAt: hoursAgo(3),
  setAt: hoursAgo(48),
};

function tokenInput(): HTMLInputElement {
  return screen.getByLabelText("media-user-token");
}

describe("AppleMusicPanel", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("shows a never-connected account without disconnect or check", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({ state: "never" });
    render(<AppleMusicPanel />);

    expect(await screen.findByText("Not connected yet.")).toBeInTheDocument();
    expect(tokenInput()).toHaveAttribute("type", "password");
    expect(
      screen.queryByRole("button", { name: "Disconnect" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Check now" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/media-user-token/, { selector: "li" }),
    ).toBeVisible();
    expect(
      screen.getByText(/never shown again/, { exact: false }),
    ).toBeInTheDocument();
  });

  it("shows storefront, playlists and the last check when connected", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue(connected);
    render(<AppleMusicPanel />);

    expect(
      await screen.findByText(
        "Connected · CH · 12 playlists · last checked 3 hours ago",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Disconnect" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Check now" })).toBeEnabled();
  });

  it("asks for a new token when expired", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({
      state: "expired",
      setAt: hoursAgo(100),
    });
    render(<AppleMusicPanel />);

    expect(
      await screen.findByText(
        "Your token has expired. Paste a new one below to reconnect.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Disconnect" })).toBeEnabled();
  });

  it("words an error by its kind, never with the server's text", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({
      state: "error",
      lastError: "apple music: /v1/me/library/playlists returned 500",
      errorKind: "upstream",
      setAt: hoursAgo(5),
    });
    render(<AppleMusicPanel />);

    expect(
      await screen.findByText(/^The last check failed/),
    ).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("/v1/me");
  });

  it("says a bridge error is not the user's token", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({
      state: "error",
      lastError: "apple music refused the web player's developer token",
      errorKind: "bridge",
    });
    render(<AppleMusicPanel />);

    expect(
      await screen.findByText(/Your token is fine\.$/),
    ).toBeInTheDocument();
  });

  it("keeps the form usable when the status cannot load", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockRejectedValue(
      new api.AppleMusicError(503, "sidecar down"),
    );
    render(<AppleMusicPanel />);

    expect(
      await screen.findByText(
        "Apple Music can’t be reached right now. Try again later.",
      ),
    ).toBeInTheDocument();
    expect(tokenInput()).toBeEnabled();
  });

  it("saves a token, clears the input and shows connected", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({ state: "never" });
    let resolveSave!: (s: api.AppleMusicStatus) => void;
    const save = vi.spyOn(api, "setAppleMusicToken").mockReturnValue(
      new Promise((resolve) => {
        resolveSave = resolve;
      }),
    );
    render(<AppleMusicPanel />);
    await screen.findByText("Not connected yet.");

    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeDisabled();
    fireEvent.change(tokenInput(), { target: { value: "  secret-token  " } });
    fireEvent.click(saveButton);

    expect(save).toHaveBeenCalledWith("secret-token");
    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();

    resolveSave(connected);
    expect(
      await screen.findByText(
        "Connected · CH · 12 playlists · last checked 3 hours ago",
      ),
    ).toBeInTheDocument();
    expect(tokenInput()).toHaveValue("");
    expect(document.body.textContent).not.toContain("secret-token");
  });

  it("shows a rejected token inline and keeps the old status", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue(connected);
    vi.spyOn(api, "setAppleMusicToken").mockRejectedValue(
      new api.AppleMusicError(422, "apple said no"),
    );
    render(<AppleMusicPanel />);
    await screen.findByText(/^Connected/);

    fireEvent.change(tokenInput(), { target: { value: "bad-token" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText(
        "Apple rejected this token. Your previous token is still in use.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/^Connected · CH/)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("bad-token");
  });

  it("on a first connect, a rejection does not claim an old token", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({ state: "never" });
    vi.spyOn(api, "setAppleMusicToken").mockRejectedValue(
      new api.AppleMusicError(422, "apple said no", "rejected"),
    );
    render(<AppleMusicPanel />);
    await screen.findByText("Not connected yet.");

    fireEvent.change(tokenInput(), { target: { value: "bad-token" } });
    fireEvent.submit(tokenInput());

    expect(
      await screen.findByText(
        "Apple rejected this token. Copy a fresh one and try again.",
      ),
    ).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("previous token");
  });

  it("says a malformed paste is no token, without blaming Apple", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({ state: "never" });
    vi.spyOn(api, "setAppleMusicToken").mockRejectedValue(
      new api.AppleMusicError(422, "not a token", "malformed"),
    );
    render(<AppleMusicPanel />);
    await screen.findByText("Not connected yet.");

    fireEvent.change(tokenInput(), { target: { value: "a=1; b=2" } });
    fireEvent.submit(tokenInput());

    expect(
      await screen.findByText(
        "That doesn’t look like a media-user-token. Paste only the cookie’s value.",
      ),
    ).toBeInTheDocument();
  });

  it("shows an unreachable Apple inline on 502", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({ state: "never" });
    vi.spyOn(api, "setAppleMusicToken").mockRejectedValue(
      new api.AppleMusicError(502, "upstream"),
    );
    render(<AppleMusicPanel />);
    await screen.findByText("Not connected yet.");

    fireEvent.change(tokenInput(), { target: { value: "tok" } });
    fireEvent.submit(tokenInput());

    expect(
      await screen.findByText(
        "Apple Music can’t be reached right now. Try again later.",
      ),
    ).toBeInTheDocument();
  });

  it("disconnects", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue(connected);
    const disconnect = vi
      .spyOn(api, "disconnectAppleMusic")
      .mockResolvedValue({ state: "never" });
    render(<AppleMusicPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));

    expect(await screen.findByText("Not connected yet.")).toBeInTheDocument();
    expect(disconnect).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByRole("button", { name: "Disconnect" }),
    ).not.toBeInTheDocument();
  });

  it("checks now with a spinner and shows the fresh status", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue({
      ...connected,
      playlistCount: 3,
    });
    let resolveCheck!: (s: api.AppleMusicStatus) => void;
    vi.spyOn(api, "checkAppleMusic").mockReturnValue(
      new Promise((resolve) => {
        resolveCheck = resolve;
      }),
    );
    render(<AppleMusicPanel />);
    await screen.findByText(/3 playlists/);

    fireEvent.click(screen.getByRole("button", { name: "Check now" }));
    const checking = screen.getByRole("button", { name: "Checking…" });
    expect(checking).toBeDisabled();
    expect(checking.querySelector(".animate-spin")).not.toBeNull();

    resolveCheck({ state: "expired", setAt: hoursAgo(1) });
    expect(
      await screen.findByText(
        "Your token has expired. Paste a new one below to reconnect.",
      ),
    ).toBeInTheDocument();
  });

  it("shows a failed check inline", async () => {
    vi.spyOn(api, "getAppleMusicStatus").mockResolvedValue(connected);
    vi.spyOn(api, "checkAppleMusic").mockRejectedValue(new Error("boom"));
    render(<AppleMusicPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Check now" }));

    expect(
      await screen.findByText("Couldn’t check the token. Try again."),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Check now" })).toBeEnabled(),
    );
  });
});
