import { request, requestJSON } from "./http";

// PushConfig mirrors GET /api/push: the key a browser subscribes with.
export type PushConfig = { publicKey: string };

// PushTestError is a test push the server could not deliver: 410 means the
// push service forgot this device, 404 that the server does not know it.
export class PushTestError extends Error {
  readonly status: number;

  constructor(status: number) {
    super("push test failed");
    this.name = "PushTestError";
    this.status = status;
  }
}

export function getPushConfig(): Promise<PushConfig> {
  return requestJSON<PushConfig>("/api/push", "push config failed");
}

// savePushSubscription stores the subscription with the VAPID key it was made
// with (applicationServerKey): the server keeps rows made with an old key
// from looking current.
export async function savePushSubscription(
  subscription: PushSubscriptionJSON,
  applicationServerKey: string,
): Promise<void> {
  await request("/api/push/subscription", "push subscribe failed", {
    method: "PUT",
    json: { ...subscription, applicationServerKey },
  });
}

export async function deletePushSubscription(endpoint: string): Promise<void> {
  await request("/api/push/subscription", "push unsubscribe failed", {
    method: "DELETE",
    json: { endpoint },
  });
}

export async function sendTestPush(
  endpoint: string,
  title: string,
  body: string,
): Promise<void> {
  const response = await request(
    "/api/push/test",
    "push test failed",
    { method: "POST", json: { endpoint, title, body } },
    [404, 410, 502],
  );
  if (!response.ok) throw new PushTestError(response.status);
}
