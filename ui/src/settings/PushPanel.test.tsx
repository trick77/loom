import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import * as device from "../pushDevice";
import { PushPanel } from "./PushPanel";

const sub = { endpoint: "https://web.push.apple.com/abc" } as PushSubscription;

function stubPermission(permission: NotificationPermission) {
  vi.stubGlobal("Notification", { permission });
}

describe("PushPanel", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    stubPermission("default");
    vi.spyOn(device, "pushSupport").mockReturnValue("supported");
    vi.spyOn(api, "getPushConfig").mockResolvedValue({ publicKey: "BKEY" });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("turns notifications on for this device", async () => {
    vi.spyOn(device, "currentPushSubscription").mockResolvedValue(null);
    const enable = vi.spyOn(device, "enablePush").mockResolvedValue(sub);
    render(<PushPanel />);

    expect(await screen.findByText("Off for this device.")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Turn on for this device" }),
    );

    expect(await screen.findByText("On for this device.")).toBeInTheDocument();
    expect(enable).toHaveBeenCalledWith("BKEY");
    expect(screen.getByRole("button", { name: "Send test" })).toBeEnabled();
  });

  it("sends a localized test to this device and turns off", async () => {
    vi.spyOn(device, "currentPushSubscription").mockResolvedValue(sub);
    const test = vi.spyOn(api, "sendTestPush").mockResolvedValue();
    const disable = vi.spyOn(device, "disablePush").mockResolvedValue();
    render(<PushPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Send test" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Test sent.");
    expect(test).toHaveBeenCalledWith(
      sub.endpoint,
      "loom",
      "Notifications work on this device.",
    );

    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    expect(await screen.findByText("Off for this device.")).toBeInTheDocument();
    expect(disable).toHaveBeenCalled();
  });

  it("says when the push service forgot the device", async () => {
    vi.spyOn(device, "currentPushSubscription").mockResolvedValue(sub);
    vi.spyOn(api, "sendTestPush").mockRejectedValue(new api.PushTestError(410));
    render(<PushPanel />);

    fireEvent.click(await screen.findByRole("button", { name: "Send test" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This device is no longer subscribed.",
    );
  });

  it("explains a declined permission and hides the button", async () => {
    vi.spyOn(device, "currentPushSubscription").mockResolvedValue(null);
    vi.spyOn(device, "enablePush").mockRejectedValue(
      new device.PushPermissionError("denied"),
    );
    render(<PushPanel />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Turn on for this device" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /blocked in this browser/,
    );
    expect(
      screen.queryByRole("button", { name: "Turn on for this device" }),
    ).not.toBeInTheDocument();
  });

  it("asks iPhone users to add loom to the Home Screen first", () => {
    vi.spyOn(device, "pushSupport").mockReturnValue("needsInstall");
    render(<PushPanel />);

    expect(screen.getByText(/Add to Home Screen/)).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(api.getPushConfig).not.toHaveBeenCalled();
  });

  it("says when the browser cannot receive pushes", () => {
    vi.spyOn(device, "pushSupport").mockReturnValue("unsupported");
    render(<PushPanel />);

    expect(
      screen.getByText("This browser can’t receive notifications."),
    ).toBeInTheDocument();
  });
});
