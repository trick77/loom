import {
  deletePushSubscription,
  getPushConfig,
  savePushSubscription,
} from "./api";

// This browser's side of Web Push: the service worker (public/sw.js) and the
// subscription the server pushes to.

export type PushSupport = "supported" | "needsInstall" | "unsupported";

const SW_URL = "/sw.js";

function isIOS(): boolean {
  return (
    /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    // iPadOS reports itself as a Mac.
    (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1)
  );
}

function isStandalone(): boolean {
  return (
    window.matchMedia?.("(display-mode: standalone)").matches === true ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true
  );
}

// pushSupport tells whether this browser can receive pushes. iOS offers Web
// Push only to a site added to the Home Screen.
export function pushSupport(): PushSupport {
  if (
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  )
    return "supported";
  return isIOS() && !isStandalone() ? "needsInstall" : "unsupported";
}

export function base64UrlToBytes(value: string): Uint8Array<ArrayBuffer> {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64 + "=".repeat((4 - (base64.length % 4)) % 4);
  const raw = atob(padded);
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

function sameKey(sub: PushSubscription, publicKey: string): boolean {
  const key = sub.options?.applicationServerKey;
  if (!key) return false;
  const have = new Uint8Array(key);
  const want = base64UrlToBytes(publicKey);
  return have.length === want.length && have.every((b, i) => b === want[i]);
}

export async function currentPushSubscription(): Promise<PushSubscription | null> {
  if (pushSupport() !== "supported") return null;
  const registration = await navigator.serviceWorker.getRegistration("/");
  return registration ? registration.pushManager.getSubscription() : null;
}

// PushPermissionError is the user (or the browser) refusing notifications.
export class PushPermissionError extends Error {
  readonly permission: NotificationPermission;

  constructor(permission: NotificationPermission) {
    super("notification permission not granted");
    this.name = "PushPermissionError";
    this.permission = permission;
  }
}

// subscribe returns a subscription for publicKey, replacing one made with an
// older key (a rotated session secret derives a new one).
async function subscribe(publicKey: string): Promise<PushSubscription> {
  await navigator.serviceWorker.register(SW_URL);
  const registration = await navigator.serviceWorker.ready;
  let sub = await registration.pushManager.getSubscription();
  if (sub && !sameKey(sub, publicKey)) {
    await sub.unsubscribe();
    sub = null;
  }
  sub ??= await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: base64UrlToBytes(publicKey),
  });
  await savePushSubscription(sub.toJSON(), publicKey);
  return sub;
}

// OWNER_KEY remembers which loom user turned push on in this browser. A visit
// re-saves the subscription only for that user: a session that expired
// without a sign-out must not hand the device's pushes to whoever signs in
// next.
const OWNER_KEY = "loom.push.owner";

function readOwner(): string | null {
  try {
    return window.localStorage.getItem(OWNER_KEY);
  } catch {
    return null;
  }
}

function writeOwner(userID: string | null): void {
  try {
    if (userID === null) window.localStorage.removeItem(OWNER_KEY);
    else window.localStorage.setItem(OWNER_KEY, userID);
  } catch {
    // No storage: a later visit treats the device as not owned and turns it
    // off, the safe side.
  }
}

// enablePush asks for the permission first, before any await: Safari grants
// the prompt only inside the tap that asked for it.
export async function enablePush(
  publicKey: string,
  userID: string,
): Promise<PushSubscription> {
  const permission = await Notification.requestPermission();
  if (permission !== "granted") throw new PushPermissionError(permission);
  const sub = await subscribe(publicKey);
  writeOwner(userID);
  return sub;
}

// unsubscribeBrowser stops this browser's pushes and returns the endpoint it
// had, null when it had none.
async function unsubscribeBrowser(): Promise<string | null> {
  const sub = await currentPushSubscription();
  writeOwner(null);
  if (!sub) return null;
  const endpoint = sub.endpoint;
  await sub.unsubscribe();
  return endpoint;
}

// disablePush unsubscribes the browser first, whatever the server then
// answers: an expired session, a network error or a hang must not leave the
// device subscribed (the next person at a shared device would see the
// previous user's pushes). A row the server keeps dies at its next push.
export async function disablePush(): Promise<void> {
  const endpoint = await unsubscribeBrowser();
  if (endpoint !== null) await deletePushSubscription(endpoint);
}

// listenForPushNavigation routes this tab where a tapped notification points
// (public/sw.js posts {type: "loom:navigate", url}), through the router's own
// popstate path: no reload. Returns the unsubscribe.
export function listenForPushNavigation(): () => void {
  if (!("serviceWorker" in navigator)) return () => {};
  const onMessage = (event: MessageEvent) => {
    const data = event.data as { type?: unknown; url?: unknown } | null;
    if (data?.type !== "loom:navigate" || typeof data.url !== "string") return;
    if (!data.url.startsWith("/") || data.url.startsWith("//")) return;
    if (window.location.pathname !== data.url)
      window.history.pushState({}, "", data.url);
    window.dispatchEvent(new PopStateEvent("popstate"));
  };
  navigator.serviceWorker.addEventListener("message", onMessage);
  return () =>
    navigator.serviceWorker.removeEventListener("message", onMessage);
}

// syncPush re-saves this browser's subscription on each visit, so the server
// follows a renewed endpoint or key. Only for the user who turned it on: for
// anyone else the browser is unsubscribed instead. Best effort.
export async function syncPush(userID: string): Promise<void> {
  try {
    if (pushSupport() !== "supported" || Notification.permission !== "granted")
      return;
    if (!(await currentPushSubscription())) return;
    if (readOwner() !== userID) {
      await unsubscribeBrowser();
      return;
    }
    const { publicKey } = await getPushConfig();
    await subscribe(publicKey);
  } catch {
    // The settings panel shows the state; a failed sync retries next visit.
  }
}

// within resolves when work settles or after ms, whichever comes first; it
// never rejects.
async function within(work: Promise<unknown>, ms: number): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      work.catch(() => {}),
      new Promise<void>((resolve) => {
        timer = setTimeout(resolve, ms);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

// forgetPushOnSignOut stops this device's pushes before a sign-out: the next
// person at a shared device must not see the previous user's notifications.
// The browser's own unsubscribe gets the longer wait, the part that protects
// the device; the server's DELETE a short one (a row it keeps dies at its
// next push). A hang must not block sign-out.
export async function forgetPushOnSignOut(): Promise<void> {
  let endpoint: string | null = null;
  await within(
    unsubscribeBrowser().then((e) => {
      endpoint = e;
    }),
    UNSUBSCRIBE_WAIT_MS,
  );
  if (endpoint !== null)
    await within(deletePushSubscription(endpoint), DELETE_WAIT_MS);
}

const UNSUBSCRIBE_WAIT_MS = 10_000;
const DELETE_WAIT_MS = 3000;
