import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import { StravaPanel } from "./StravaPanel";

const hoursAgo = (h: number) =>
  new Date(Date.now() - h * 3_600_000).toISOString();

const connected: api.StravaStatus = {
  state: "connected",
  athlete: { id: 7, name: "Jan R." },
  scopes: ["read_all", "profile:read_all", "activity:read_all"],
  lastOkAt: hoursAgo(2),
};

describe("StravaPanel", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("offers Connect with Strava when never connected", async () => {
    vi.spyOn(api, "getStravaStatus").mockResolvedValue({ state: "never" });
    render(<StravaPanel />);

    expect(await screen.findByText("Not connected yet.")).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Connect with Strava" });
    expect(link).toHaveAttribute("href", "/api/me/strava/connect");
    expect(
      screen.queryByRole("button", { name: "Disconnect" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/Only you see your activities/)).toBeVisible();
  });

  it("names the athlete and offers check and disconnect when connected", async () => {
    vi.spyOn(api, "getStravaStatus").mockResolvedValue(connected);
    render(<StravaPanel />);

    expect(
      await screen.findByText("Connected as Jan R. · last checked 2 hours ago"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Check now" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Disconnect" })).toBeEnabled();
    expect(
      screen.queryByRole("link", { name: /Connect|Reconnect/ }),
    ).not.toBeInTheDocument();
  });

  it("asks to reconnect when permissions are missing", async () => {
    vi.spyOn(api, "getStravaStatus").mockResolvedValue({
      ...connected,
      missingScopes: ["activity:read_all"],
    });
    render(<StravaPanel />);

    expect(
      await screen.findByText(/were not granted \(activity:read_all\)/),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Reconnect" })).toBeVisible();
  });

  it("asks to reconnect when Strava ended the sign-in", async () => {
    vi.spyOn(api, "getStravaStatus").mockResolvedValue({ state: "expired" });
    render(<StravaPanel />);

    expect(
      await screen.findByText(/Strava ended the sign-in/),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Reconnect" })).toBeVisible();
  });

  it("shows the outcome of Strava's consent page", async () => {
    vi.spyOn(api, "getStravaStatus").mockResolvedValue({ state: "never" });
    render(<StravaPanel outcome="denied" />);

    expect(
      await screen.findByText("Strava wasn’t connected: access was declined."),
    ).toBeInTheDocument();
  });

  it("disconnects and shows the never state", async () => {
    vi.spyOn(api, "getStravaStatus").mockResolvedValue(connected);
    const disconnect = vi
      .spyOn(api, "disconnectStrava")
      .mockResolvedValue({ state: "never" });
    render(<StravaPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
    await waitFor(() => expect(disconnect).toHaveBeenCalled());
    expect(await screen.findByText("Not connected yet.")).toBeInTheDocument();
  });

  it("words a failed check by code, never with the server's text", async () => {
    vi.spyOn(api, "getStravaStatus").mockResolvedValue(connected);
    vi.spyOn(api, "checkStrava").mockRejectedValue(
      new api.StravaError(502, "strava: GET /athlete 500", "upstream"),
    );
    render(<StravaPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Check now" }));
    expect(
      await screen.findByText(
        "Strava can’t be reached right now. Try again later.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/GET \/athlete/)).not.toBeInTheDocument();
  });

  it("says when the status cannot be loaded", async () => {
    vi.spyOn(api, "getStravaStatus").mockRejectedValue(
      new api.StravaError(503, "down", "unavailable"),
    );
    render(<StravaPanel />);

    expect(
      await screen.findByText(
        "loom’s Strava service isn’t running right now. Try again later.",
      ),
    ).toBeInTheDocument();
  });
});

describe("readStravaOutcome / clearStravaOutcome", () => {
  afterEach(() => window.history.replaceState(null, "", "/"));

  it("reads ?strava= without touching the URL; clear removes only it", () => {
    window.history.replaceState(null, "", "/?strava=connected&x=1#h");
    expect(api.readStravaOutcome()).toBe("connected");
    expect(api.readStravaOutcome()).toBe("connected");
    api.clearStravaOutcome();
    expect(window.location.search).toBe("?x=1");
    expect(window.location.hash).toBe("#h");
    expect(api.readStravaOutcome()).toBeNull();
  });

  it("treats an unknown outcome as unavailable", () => {
    window.history.replaceState(null, "", "/?strava=<script>");
    expect(api.readStravaOutcome()).toBe("unavailable");
  });
});
