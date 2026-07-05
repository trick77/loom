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
  // Marks the recommended default (the "Default" pill). The label and description
  // are localized in ReasoningMenu via i18n keys (composer.reasoning.*), so they
  // are not stored here.
  default?: boolean;
};

// Ordered high -> low so the recommended default sits at the top of the menu.
export const REASONING_OPTIONS: ReasoningOption[] = [
  { value: "high", default: true },
  { value: "medium" },
  { value: "low" },
];

export function isReasoningEffort(value: unknown): value is ReasoningEffort {
  return value === "low" || value === "medium" || value === "high";
}
