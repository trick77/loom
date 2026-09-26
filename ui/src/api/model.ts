// The chat model's facts as the backend reads them from llmwire's profile.
// Public endpoint: shared pages need the context window too.
export interface ModelInfo {
  model: string;
  displayName: string;
  contextWindow: number;
}

let pending: Promise<ModelInfo | null> | null = null;

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
        return info;
      });
  }
  return pending;
}

export function resetModelInfoForTest(): void {
  pending = null;
}
