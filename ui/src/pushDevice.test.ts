import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "./api";
import {
  PushPermissionError,
  base64UrlToBytes,
  enablePush,
  forgetPushOnSignOut,
  listenForPushNavigation,
  pushSupport,
  syncPush,
} from "./pushDevice";

const KEY = "BAEC"; // base64url of 04 01 02

type FakeSub = {
  endpoint: string;
  options: { applicationServerKey: ArrayBuffer | null };
  toJSON(): PushSubscriptionJSON;
  unsubscribe: ReturnType<typeof vi.fn>;
};

function fakeSub(endpoint: string, key: Uint8Array): FakeSub {
  return {
    endpoint,
    options: { applicationServerKey: key.slice().buffer },
    toJSON: () => ({ endpoint, keys: { p256dh: "p", auth: "a" } }),
    unsubscribe: vi.fn().mockResolvedValue(true),
  };
}

function installBrowser(existing: FakeSub | null, permission = "granted") {
  let current = existing;
  const pushManager = {
    getSubscription: vi.fn(async () => current),
    subscribe: vi.fn(async (opts: { applicationServerKey: Uint8Array }) => {
      current = fakeSub(
        "https://web.push.apple.com/new",
        opts.applicationServerKey,
      );
      return current;
    }),
  };
  const registration = { pushManager };
  vi.stubGlobal("navigator", {
    userAgent: "test",
    platform: "MacIntel",
    maxTouchPoints: 0,
    serviceWorker: {
      register: vi.fn(async () => registration),
      ready: Promise.resolve(registration),
      getRegistration: vi.fn(async () => registration),
    },
  });
  vi.stubGlobal("PushManager", function PushManager() {});
  vi.stubGlobal("Notification", {
    permission,
    requestPermission: vi.fn(async () => permission),
  });
  return pushManager;
}

describe("pushDevice", () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => vi.unstubAllGlobals());

  it("decodes base64url keys", () => {
    expect(Array.from(base64UrlToBytes("BAEC"))).toEqual([4, 1, 2]);
    expect(Array.from(base64UrlToBytes("_-8"))).toEqual([255, 239]);
  });

  it("subscribes with the server key and saves the subscription", async () => {
    const pm = installBrowser(null);
    const save = vi.spyOn(api, "savePushSubscription").mockResolvedValue();

    const sub = await enablePush(KEY);

    expect(sub.endpoint).toBe("https://web.push.apple.com/new");
    expect(pm.subscribe).toHaveBeenCalledWith(
      expect.objectContaining({ userVisibleOnly: true }),
    );
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({ endpoint: "https://web.push.apple.com/new" }),
    );
  });

  it("refuses without the permission", async () => {
    installBrowser(null, "denied");
    await expect(enablePush(KEY)).rejects.toBeInstanceOf(PushPermissionError);
  });

  it("re-saves an existing subscription on a visit", async () => {
    const existing = fakeSub(
      "https://web.push.apple.com/old",
      base64UrlToBytes(KEY),
    );
    const pm = installBrowser(existing);
    vi.spyOn(api, "getPushConfig").mockResolvedValue({ publicKey: KEY });
    const save = vi.spyOn(api, "savePushSubscription").mockResolvedValue();

    await syncPush();

    expect(pm.subscribe).not.toHaveBeenCalled();
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({ endpoint: "https://web.push.apple.com/old" }),
    );
  });

  it("renews a subscription made with an older key", async () => {
    const existing = fakeSub(
      "https://web.push.apple.com/old",
      new Uint8Array([4, 9, 9]),
    );
    const pm = installBrowser(existing);
    vi.spyOn(api, "getPushConfig").mockResolvedValue({ publicKey: KEY });
    const save = vi.spyOn(api, "savePushSubscription").mockResolvedValue();

    await syncPush();

    expect(existing.unsubscribe).toHaveBeenCalled();
    expect(pm.subscribe).toHaveBeenCalled();
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({ endpoint: "https://web.push.apple.com/new" }),
    );
  });

  it("does nothing on a visit without a subscription", async () => {
    installBrowser(null);
    const config = vi.spyOn(api, "getPushConfig");
    await syncPush();
    expect(config).not.toHaveBeenCalled();
  });

  it("forgets this device on sign-out", async () => {
    const existing = fakeSub(
      "https://web.push.apple.com/old",
      base64UrlToBytes(KEY),
    );
    installBrowser(existing);
    const del = vi.spyOn(api, "deletePushSubscription").mockResolvedValue();

    await forgetPushOnSignOut();

    expect(del).toHaveBeenCalledWith("https://web.push.apple.com/old");
    expect(existing.unsubscribe).toHaveBeenCalled();
  });

  it("unsubscribes the browser even when the server refuses", async () => {
    const existing = fakeSub(
      "https://web.push.apple.com/old",
      base64UrlToBytes(KEY),
    );
    installBrowser(existing);
    vi.spyOn(api, "deletePushSubscription").mockRejectedValue(
      new api.AuthExpiredError(),
    );

    await forgetPushOnSignOut();

    expect(existing.unsubscribe).toHaveBeenCalled();
  });

  it("does not hold up sign-out when the server hangs", async () => {
    vi.useFakeTimers();
    try {
      installBrowser(
        fakeSub("https://web.push.apple.com/old", base64UrlToBytes(KEY)),
      );
      vi.spyOn(api, "deletePushSubscription").mockReturnValue(
        new Promise(() => {}),
      );
      const done = forgetPushOnSignOut();
      await vi.advanceTimersByTimeAsync(3000);
      await expect(done).resolves.toBeUndefined();
    } finally {
      vi.useRealTimers();
    }
  });

  it("routes a tapped notification without reloading", () => {
    const target = new EventTarget();
    vi.stubGlobal("navigator", {
      serviceWorker: {
        addEventListener: target.addEventListener.bind(target),
        removeEventListener: target.removeEventListener.bind(target),
      },
    });
    const popstate = vi.fn();
    window.addEventListener("popstate", popstate);
    const stop = listenForPushNavigation();
    try {
      target.dispatchEvent(
        new MessageEvent("message", {
          data: { type: "loom:navigate", url: "//evil.example/x" },
        }),
      );
      expect(popstate).not.toHaveBeenCalled();

      target.dispatchEvent(
        new MessageEvent("message", {
          data: { type: "loom:navigate", url: "/schedules/abc" },
        }),
      );
      expect(window.location.pathname).toBe("/schedules/abc");
      expect(popstate).toHaveBeenCalledTimes(1);
    } finally {
      stop();
      window.removeEventListener("popstate", popstate);
      window.history.replaceState({}, "", "/");
    }
  });

  it("tells iPhone Safari to install loom first", () => {
    vi.stubGlobal("navigator", {
      userAgent: "iPhone",
      platform: "iPhone",
      maxTouchPoints: 5,
    });
    expect(pushSupport()).toBe("needsInstall");
  });
});
