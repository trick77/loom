// loom's service worker. It shows the Web Push messages the server sends
// (backend/internal/httpapi/push_handlers.go: {title, body, url, tag}) and
// opens a notification's page on a tap. It caches nothing.
/* global self, URL, fetch */

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
      const tab = windows.find(
        (client) => new URL(client.url).origin === self.location.origin,
      );
      if (tab) {
        await tab.focus();
        tab.postMessage({ type: "loom:navigate", url: path });
        return;
      }
      await self.clients.openWindow(new URL(path, self.location.origin).href);
    })(),
  );
});

// The browser renewed or dropped the subscription: hand the current one to
// the server. Chrome often fires this without oldSubscription; the key then
// comes from the server.
self.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(
    (async () => {
      let sub = event.newSubscription;
      if (!sub) {
        let options = event.oldSubscription && event.oldSubscription.options;
        if (!options || !options.applicationServerKey) {
          const response = await fetch("/api/push", {
            credentials: "same-origin",
          });
          if (!response.ok) return;
          const { publicKey } = await response.json();
          options = {
            userVisibleOnly: true,
            applicationServerKey: publicKey,
          };
        }
        sub = await self.registration.pushManager.subscribe(options);
      }
      await fetch("/api/push/subscription", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(sub.toJSON()),
      });
    })(),
  );
});
