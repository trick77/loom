import {
  AuthExpiredError,
  request,
  requestJSON,
  UserFacingError,
} from "./http";

export type ScheduleRunStatus =
  "running" | "new" | "nothing_new" | "token_limit" | "timeout" | "failed";

export type ScheduleRun = {
  id: string;
  scheduleId: string;
  /** The thread the run started; null once deleted. */
  threadId: string | null;
  messageId: string | null;
  startedAt: string;
  finishedAt: string | null;
  status: ScheduleRunStatus;
  newCount: number;
  rounds: number;
  tokens: number;
  durationMs: number;
  error: string;
  newLabels: string[];
};

export type Schedule = {
  id: string;
  name: string;
  prompt: string;
  interval: string;
  cron: string;
  template: string;
  timezone: string;
  enabled: boolean;
  nextRunAt: string | null;
  running: boolean;
  disabledReason: string;
  disabledAt: string | null;
  createdAt: string;
  updatedAt: string;
  summary: string;
  nextRuns: string[];
  lastRun: ScheduleRun | null;
};

export type SchedulePreview = {
  interval: string;
  template: string;
  cron: string;
  timezone: string;
  summary: string;
  nextRuns: string[];
};

export type ScheduleFinding = {
  key: string;
  label: string;
  sourceUrl: string;
  firstSeenAt: string;
};

export type ScheduleSource = {
  url: string;
  prefer: "fetch" | "browser";
  fetchFailedAt: string | null;
  browserOkAt: string | null;
  runs: number;
  finds: number;
  lastUsedAt: string;
};

export type ScheduleQuery = {
  query: string;
  runs: number;
  finds: number;
  emptyRuns: number;
  lastUsedAt: string;
};

export type ScheduleMemory = {
  findings: ScheduleFinding[];
  sources: ScheduleSource[];
  queries: ScheduleQuery[];
};

export type ScheduleMemoryKind = "findings" | "sources" | "queries";

export type AppNotification = {
  id: string;
  kind: "schedule_run" | "schedule_disabled";
  scheduleId: string | null;
  threadId: string | null;
  messageId: string | null;
  title: string;
  body: string;
  createdAt: string;
  readAt: string | null;
};

// ScheduleIntervalError is an interval the server would not schedule: too
// often, or not a time at all. The form shows it under the interval field.
export class ScheduleIntervalError extends Error {
  constructor(
    readonly code: "too_often" | "not_understood",
    message: string,
  ) {
    super(message);
    this.name = "ScheduleIntervalError";
  }
}

// browserTimezone is the IANA zone schedule slots are drawn in.
export function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

const scheduleUrl = (id: string) => `/api/schedules/${encodeURIComponent(id)}`;

// sendSchedule posts a schedule body and maps the server's interval and
// validation errors to ones the form can show.
async function sendSchedule<T>(
  url: string,
  method: string,
  body: unknown,
  errorMessage: string,
): Promise<T> {
  const response = await fetch(url, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (response.status === 401) throw new AuthExpiredError();
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as {
      error?: string;
      code?: string;
    };
    if (data.code === "too_often" || data.code === "not_understood") {
      throw new ScheduleIntervalError(data.code, data.error ?? errorMessage);
    }
    if ((response.status === 400 || response.status === 409) && data.error) {
      throw new UserFacingError(data.error);
    }
    throw new Error(errorMessage);
  }
  return response.json() as Promise<T>;
}

export async function listSchedules(): Promise<Schedule[]> {
  return requestJSON(`/api/schedules`, "failed to load schedules");
}

export async function getSchedule(id: string): Promise<Schedule> {
  return requestJSON(scheduleUrl(id), "failed to load schedule");
}

export async function previewSchedule(
  interval: string,
): Promise<SchedulePreview> {
  return sendSchedule(
    `/api/schedules:parse`,
    "POST",
    { interval, timezone: browserTimezone() },
    "failed to read the interval",
  );
}

export type ScheduleInput = {
  name?: string;
  prompt?: string;
  interval?: string;
  enabled?: boolean;
  template?: string;
  cron?: string;
  /** The IANA zone a drawn slot was previewed in. */
  timezone?: string;
  reroll?: boolean;
};

export async function createSchedule(input: ScheduleInput): Promise<Schedule> {
  return sendSchedule(
    `/api/schedules`,
    "POST",
    { timezone: browserTimezone(), ...input },
    "failed to create schedule",
  );
}

export async function updateSchedule(
  id: string,
  input: ScheduleInput,
): Promise<Schedule> {
  return sendSchedule(
    scheduleUrl(id),
    "PATCH",
    input,
    "failed to update schedule",
  );
}

export async function deleteSchedule(id: string): Promise<void> {
  await request(scheduleUrl(id), "failed to delete schedule", {
    method: "DELETE",
  });
}

export async function runScheduleNow(id: string): Promise<void> {
  const response = await fetch(`${scheduleUrl(id)}/run`, { method: "POST" });
  if (response.status === 401) throw new AuthExpiredError();
  if (response.status === 409) {
    const data = (await response.json().catch(() => ({}))) as {
      error?: string;
    };
    throw new UserFacingError(data.error ?? "already running");
  }
  if (!response.ok) throw new Error("failed to start the run");
}

export async function listScheduleRuns(id: string): Promise<ScheduleRun[]> {
  return requestJSON(`${scheduleUrl(id)}/runs`, "failed to load runs");
}

export async function getScheduleMemory(id: string): Promise<ScheduleMemory> {
  return requestJSON(`${scheduleUrl(id)}/memory`, "failed to load memory");
}

// forgetScheduleMemory forgets one memory item, or the whole kind when key is
// left out.
export async function forgetScheduleMemory(
  id: string,
  kind: ScheduleMemoryKind,
  key?: string,
): Promise<void> {
  const query = key ? `?key=${encodeURIComponent(key)}` : "";
  await request(
    `${scheduleUrl(id)}/memory/${kind}${query}`,
    "failed to forget",
    { method: "DELETE" },
  );
}

export async function listNotifications(): Promise<AppNotification[]> {
  return requestJSON(`/api/notifications`, "failed to load notifications");
}

export async function unreadNotificationCount(): Promise<number> {
  const data = await requestJSON<{ count: number }>(
    `/api/notifications/unread`,
    "failed to load notifications",
  );
  return data.count;
}

export async function markNotificationRead(id: string): Promise<void> {
  await request(
    `/api/notifications/${encodeURIComponent(id)}/read`,
    "failed to mark read",
    { method: "POST" },
  );
}

export async function markAllNotificationsRead(): Promise<void> {
  await request(`/api/notifications/read-all`, "failed to mark read", {
    method: "POST",
  });
}
