// Reasoning-effort selection for the composer. Loom serves a single model
// (MiMo-V2.5-Pro) but exposes MiMo's reasoning_effort scale so the user can trade
// depth for speed per turn. The backend clamps unknown values to the default, so
// this list is the single source of truth for what the UI offers.

export type ReasoningEffort = "low" | "medium" | "high";

// High is the model's own default; the composer opens on it and the "Standard"
// pill marks it as the recommended choice.
export const DEFAULT_REASONING_EFFORT: ReasoningEffort = "high";

// localStorage key for the persisted reasoning-effort choice.
export const REASONING_EFFORT_STORAGE_KEY = "loom.reasoningEffort";

// The only model Loom serves, shown as a static (non-interactive) label in the
// composer — there is no model picker. This is Xiaomi's official spelling
// (mimo.xiaomi.com / huggingface.co/XiaomiMiMo), matching the backend's
// mimo-v2.5-pro model tag.
export const MODEL_LABEL = "MiMo-V2.5-Pro";

export type ReasoningOption = {
  value: ReasoningEffort;
  label: string;
  description: string;
  // Short pill shown beside the label (only "Standard" today, marking the default).
  badge?: string;
};

// Ordered high -> low so the recommended default sits at the top of the menu.
// The copy is explicit that this is a speed-for-quality trade: less reasoning
// means faster replies but weaker answers. On an unlimited plan there is nothing
// to say about cost or quotas, so that stays out of the copy entirely.
export const REASONING_OPTIONS: ReasoningOption[] = [
  {
    value: "high",
    label: "High",
    badge: "Standard",
    description:
      "The most reasoning and the strongest answers, at the cost of speed. Best for hard coding, multi-step problems, and tricky edge cases. The default.",
  },
  {
    value: "medium",
    label: "Medium",
    description:
      "Less reasoning for faster replies, with some loss of answer quality. A fair balance for everyday tasks that are not especially hard.",
  },
  {
    value: "low",
    label: "Low",
    description:
      "The least reasoning and the fastest replies, but the weakest answers. Best kept for quick, straightforward asks.",
  },
];

export function isReasoningEffort(value: unknown): value is ReasoningEffort {
  return value === "low" || value === "medium" || value === "high";
}

export function reasoningLabel(effort: ReasoningEffort): string {
  return REASONING_OPTIONS.find((option) => option.value === effort)?.label ?? "High";
}
