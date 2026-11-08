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

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const target = new URL(
    samePath(event.notification.data && event.notification.data.url),
    self.location.origin,
  ).href;
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      for (const client of windows) {
        if (new URL(client.url).origin !== self.location.origin) continue;
        try {
          await client.focus();
          await client.navigate(target);
          return;
        } catch {
          // An uncontrolled window cannot be navigated: open a new one.
        }
      }
      await self.clients.openWindow(target);
    })(),
  );
});

// The browser renewed the subscription: hand the new one to the server.
self.addEventListener("pushsubscriptionchange", (event) => {
  const options = event.oldSubscription && event.oldSubscription.options;
  if (!options) return;
  event.waitUntil(
    self.registration.pushManager.subscribe(options).then((sub) =>
      fetch("/api/push/subscription", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(sub.toJSON()),
      }),
    ),
  );
});
