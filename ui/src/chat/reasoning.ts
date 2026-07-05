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
// composer — there is no model picker. Display form of Xiaomi's MiMo-V2.5-Pro
// (mimo.xiaomi.com), reading like a name rather than the model tag.
export const MODEL_LABEL = "MiMo 2.5 Pro";

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
      "Thinks the most, for the most accurate and thorough answers. Best for coding, math, and careful analysis. A little slower to respond. The default.",
  },
  {
    value: "medium",
    label: "Medium",
    description:
      "Thinks less and replies faster. Still reliable for everyday questions and writing, just less thorough.",
  },
  {
    value: "low",
    label: "Low",
    description:
      "Thinks the least and replies fastest, so answers are more likely to miss details or slip up. Best for simple questions and quick edits.",
  },
];

export function isReasoningEffort(value: unknown): value is ReasoningEffort {
  return value === "low" || value === "medium" || value === "high";
}

export function reasoningLabel(effort: ReasoningEffort): string {
  return REASONING_OPTIONS.find((option) => option.value === effort)?.label ?? "High";
}
