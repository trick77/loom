// The chat model's facts as the backend reads them from llmwire's profile.
// Public endpoint: shared pages need the context window too.
export interface ModelInfo {
  model: string;
  displayName: string;
  contextWindow: number;
  // The choices the composer offers, standard first. One entry (or none, from
  // an older backend) means there is nothing to pick.
  slots?: ChatSlot[];
  // The distinct models an answer can come from: the slots' and the vision
  // model an image turn falls back to. Absent from an older backend.
  answering?: AnsweringModel[];
}

// One composer choice: the key the client sends, the model behind it.
export interface ChatSlot {
  key: string;
  model: string;
  displayName: string;
  contextWindow: number;
}

// One model an answer can come from.
export interface AnsweringModel {
  model: string;
  displayName: string;
  contextWindow: number;
}

export const STANDARD_SLOT = "standard";

let pending: Promise<ModelInfo | null> | null = null;
let resolved: ModelInfo | null = null;

// getModelInfo fetches /api/model once per page and shares the answer. A
// failure resolves null (the context % is then simply omitted) and clears the
// cache so a later call can try again.
export function getModelInfo(): Promise<ModelInfo | null> {
  if (pending === null) {
    pending = fetch("/api/model")
      .then((response) => (response.ok ? response.json() : null))
      .catch(() => null)
      .then((info: ModelInfo | null) => {
        if (info === null) pending = null;
        resolved = info;
        return info;
      });
  }
  return pending;
}

// peekModelInfo is the answer getModelInfo already has, or null.
export function peekModelInfo(): ModelInfo | null {
  return resolved;
}

export function resetModelInfoForTest(): void {
  pending = null;
  resolved = null;
}

function answeringModels(info: ModelInfo | null): AnsweringModel[] {
  return info?.answering ?? info?.slots ?? [];
}

// contextWindowFor is a model's context window in tokens: the configured
// model whose id matches, else the chat model's (a message from a model no
// longer configured, or none named); undefined until the info is known.
export function contextWindowFor(
  info: ModelInfo | null,
  model?: string,
): number | undefined {
  const match = answeringModels(info).find((m) => m.model === model);
  return match?.contextWindow ?? info?.contextWindow;
}

// modelNameFor is the display name of the model that wrote a message, or
// undefined when only one model can answer (nothing to tell apart; a deep
// slot on the chat model counts once) or the model is not configured any
// more.
export function modelNameFor(
  info: ModelInfo | null,
  model?: string,
): string | undefined {
  const models = answeringModels(info);
  if (new Set(models.map((m) => m.model)).size < 2) return undefined;
  return models.find((m) => m.model === model)?.displayName;
}
