import {
  request,
  requestJSON,
  UserFacingError,
  AuthExpiredError,
} from "./http";
import { browserTimezone } from "./schedules";

export type IdeaState = "open" | "done";

export type Idea = {
  id: string;
  state: IdeaState;
  title: string;
  reason: string;
  /** The captured passage: outside text, always rendered as plain text. */
  excerpt: string;
  threadId: string | null;
  messageId: string | null;
  /** The thread's title when the idea was captured; kept after a delete. */
  threadTitle: string;
  threadDeleted: boolean;
  /** 'YYYY-MM-DD' civil dates in the user's zone. */
  dueOn: string | null;
  remindOn: string | null;
  doneAt: string | null;
  createdAt: string;
};

export type IdeaCounts = { open: number; done: number };

/**
 * A new idea. Dates are tri-state on the wire: a key left out keeps the
 * server's default, null clears, a date sets it.
 */
export type IdeaInput = {
  title?: string;
  reason?: string;
  excerpt?: string;
  threadId?: string;
  messageId?: string;
  dueOn?: string | null;
  remindOn?: string | null;
};

export type IdeaPatch = Omit<IdeaInput, "threadId" | "messageId"> & {
  done?: boolean;
  remindDismissed?: boolean;
};

// IDEAS_CHANGED has the sidebar recount after an idea was saved, deleted,
// restored or marked done anywhere in the app.
export const IDEAS_CHANGED = "loom:ideas-changed";

const ideaUrl = (id: string) => `/api/ideas/${encodeURIComponent(id)}`;

// The zone makes "today" the user's day: follow-up dates and their order.
const zoneHeaders = () => ({ "X-Loom-Timezone": browserTimezone() });

// sendIdea writes an idea and turns the server's validation text into an
// error the form can show.
async function sendIdea(
  url: string,
  method: string,
  body: unknown,
  errorMessage: string,
): Promise<Idea> {
  const response = await fetch(url, {
    method,
    headers: { "Content-Type": "application/json", ...zoneHeaders() },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 401) throw new AuthExpiredError();
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as {
      error?: string;
    };
    if (response.status === 400 && data.error) {
      throw new UserFacingError(data.error);
    }
    throw new Error(errorMessage);
  }
  return response.json() as Promise<Idea>;
}

export async function listIdeas(status: IdeaState): Promise<Idea[]> {
  const data = await requestJSON<{ items: Idea[] }>(
    `/api/ideas?status=${status}`,
    "failed to load ideas",
    { headers: zoneHeaders() },
  );
  return data.items;
}

export async function countIdeas(): Promise<IdeaCounts> {
  return requestJSON(`/api/ideas/count`, "failed to count ideas");
}

export async function getIdea(id: string): Promise<Idea> {
  return requestJSON(ideaUrl(id), "failed to load idea");
}

export async function createIdea(input: IdeaInput): Promise<Idea> {
  return sendIdea(`/api/ideas`, "POST", input, "failed to save idea");
}

export async function updateIdea(id: string, patch: IdeaPatch): Promise<Idea> {
  return sendIdea(ideaUrl(id), "PATCH", patch, "failed to update idea");
}

// deleteIdea tolerates a 404: another tab deleted it already.
export async function deleteIdea(id: string): Promise<void> {
  await request(
    ideaUrl(id),
    "failed to delete idea",
    { method: "DELETE" },
    [404],
  );
}

export async function restoreIdea(id: string): Promise<Idea> {
  return sendIdea(
    `${ideaUrl(id)}/restore`,
    "POST",
    undefined,
    "failed to restore idea",
  );
}

export async function confirmIdea(id: string): Promise<Idea> {
  return sendIdea(
    `${ideaUrl(id)}/confirm`,
    "POST",
    undefined,
    "failed to save idea",
  );
}
