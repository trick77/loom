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
  await savePushSubscription(sub.toJSON());
  return sub;
}

// enablePush asks for the permission first, before any await: Safari grants
// the prompt only inside the tap that asked for it.
export async function enablePush(publicKey: string): Promise<PushSubscription> {
  const permission = await Notification.requestPermission();
  if (permission !== "granted") throw new PushPermissionError(permission);
  return subscribe(publicKey);
}

export async function disablePush(): Promise<void> {
  const sub = await currentPushSubscription();
  if (!sub) return;
  await deletePushSubscription(sub.endpoint);
  await sub.unsubscribe();
}

// syncPush re-saves this browser's subscription on each visit, so the server
// follows a renewed endpoint or key and the user signed in now. Best effort.
export async function syncPush(): Promise<void> {
  try {
    if (pushSupport() !== "supported" || Notification.permission !== "granted")
      return;
    if (!(await currentPushSubscription())) return;
    const { publicKey } = await getPushConfig();
    await subscribe(publicKey);
  } catch {
    // The settings panel shows the state; a failed sync retries next visit.
  }
}

// forgetPushOnSignOut stops this device's pushes before a sign-out: the next
// person at a shared device must not see the previous user's notifications.
// It waits at most a few seconds: a hanging request must not block sign-out.
export async function forgetPushOnSignOut(): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      disablePush(),
      new Promise<void>((resolve) => {
        timer = setTimeout(resolve, SIGN_OUT_WAIT_MS);
      }),
    ]);
  } catch {
    // Sign-out goes ahead regardless.
  } finally {
    clearTimeout(timer);
  }
}

const SIGN_OUT_WAIT_MS = 3000;
