import { AuthExpiredError } from "./http";
import type { ActionCardData } from "./types";

// ToolActionGoneError is an action the server does not have for this user.
export class ToolActionGoneError extends Error {
  constructor() {
    super("tool action not found");
    this.name = "ToolActionGoneError";
  }
}

// ToolActionServerError is loom failing the request (5xx): the server is
// there, so "unreachable, try again" would be wrong.
export class ToolActionServerError extends Error {
  constructor(status: number) {
    super(`tool action request failed: ${status}`);
    this.name = "ToolActionServerError";
  }
}

const actionUrl = (id: string) => `/api/tool-actions/${encodeURIComponent(id)}`;

// sendAction asks for an action's card, or runs or dismisses it. An action
// no longer waiting (a second click, another tab) answers 409 with its card
// as it stands, which is what the card shows then.
async function sendAction(
  id: string,
  verb: "run" | "dismiss" | null,
): Promise<ActionCardData> {
  const response =
    verb === null
      ? await fetch(actionUrl(id))
      : await fetch(`${actionUrl(id)}/${verb}`, { method: "POST" });
  if (response.status === 401) throw new AuthExpiredError();
  if (response.status === 404) throw new ToolActionGoneError();
  if (response.status >= 500) throw new ToolActionServerError(response.status);
  if (!response.ok && response.status !== 409) {
    throw new Error(`tool action request failed: ${response.status}`);
  }
  return response.json() as Promise<ActionCardData>;
}

/** runToolAction makes the proposed call: the user's click on its card. */
export function runToolAction(id: string): Promise<ActionCardData> {
  return sendAction(id, "run");
}

/** dismissToolAction discards the proposed call. */
export function dismissToolAction(id: string): Promise<ActionCardData> {
  return sendAction(id, "dismiss");
}

/** getToolAction is the action's card as it stands. */
export function getToolAction(id: string): Promise<ActionCardData> {
  return sendAction(id, null);
}
