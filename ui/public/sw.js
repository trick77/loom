// loom's service worker. It shows the Web Push messages the server sends
// (backend/internal/httpapi/push_handlers.go: {title, body, url, tag}) and
// opens a notification's page on a tap. It caches nothing.
/* global self, URL, fetch, btoa */

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) =>
  event.waitUntil(self.clients.claim()),
);

// samePath keeps a notification's link on loom: a path, never another origin.
function samePath(url) {
  return typeof url === "string" && url.startsWith("/") && !url.startsWith("//")
    ? url
    : "/";
}

self.addEventListener("push", (event) => {
  let data = {};
  if (event.data) {
    try {
      data = event.data.json();
    } catch {
      data = { body: event.data.text() };
    }
  }
  // Every push must show a notification: Safari revokes a subscription that
  // receives silent ones.
  event.waitUntil(
    self.registration.showNotification(data.title || "loom", {
      body: data.body || "",
      icon: "/icon-192.png",
      badge: "/icon-192.png",
      tag: data.tag || undefined,
      renotify: Boolean(data.tag),
      data: { url: samePath(data.url) },
    }),
  );
});

// A tap focuses an open loom tab and asks it to route there itself
// (src/pushDevice.ts listenForPushNavigation): a navigate() would reload the
// SPA and lose a draft or a streaming answer. No loom tab: open one.
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const path = samePath(event.notification.data && event.notification.data.url);
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      const loom = windows.filter(
        (client) => new URL(client.url).origin === self.location.origin,
      );
      // The tab the user is looking at, not just the first one listed.
      const tab =
        loom.find((client) => client.focused) ||
        loom.find((client) => client.visibilityState === "visible") ||
        loom[0];
      if (tab) {
        await tab.focus();
        tab.postMessage({ type: "loom:navigate", url: path });
        return;
      }
      await self.clients.openWindow(new URL(path, self.location.origin).href);
    })(),
  );
});

function base64Url(buffer) {
  let raw = "";
  for (const b of new Uint8Array(buffer)) raw += String.fromCharCode(b);
  return btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// The browser renewed or dropped the subscription: hand the current one to
// the server, with the key it was made with. A new one is always made with
// loom's current key (an old subscription's options may hold a key a rotated
// session secret retired).
self.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(
    (async () => {
      let sub = event.newSubscription;
      let key =
        sub && sub.options.applicationServerKey
          ? base64Url(sub.options.applicationServerKey)
          : null;
      if (!sub || !key) {
        const response = await fetch("/api/push", {
          credentials: "same-origin",
        });
        if (!response.ok) return;
        key = (await response.json()).publicKey;
        if (sub) await sub.unsubscribe();
        sub = await self.registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: key,
        });
      }
      await fetch("/api/push/subscription", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ ...sub.toJSON(), applicationServerKey: key }),
      });
    })(),
  );
});
