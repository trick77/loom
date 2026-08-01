/**
 * Per-surface composer drafts.
 *
 * The unsent textarea contents and the staged "Pasted" chips used to be two
 * single state cells shared by every composer in the app, so a half-written
 * question typed in one thread reappeared in the next thread and on the start
 * screen. They are now keyed by the surface that owns them, in the same key space
 * as the streaming runs (see streamRuns.ts) so the two stay easy to reason about
 * together.
 *
 * A draft is cleared on a successful send and never on navigation — leaving a
 * thread must not throw away what you typed there. Empty drafts are pruned so the
 * record does not grow with every thread visited.
 */
import type { RouteState } from "./routing";
import type { PastedText } from "./pastedText";

export type DraftScope = string;

export type DraftState = {
  text: string;
  pastedTexts: PastedText[];
};

/** What a surface renders before anything has been typed there. Never mutate. */
export const EMPTY_DRAFT: DraftState = Object.freeze({
  text: "",
  pastedTexts: Object.freeze([]) as unknown as PastedText[],
}) as DraftState;

export type ComposerDrafts = Record<DraftScope, DraftState>;

export const INCOGNITO_DRAFT_SCOPE: DraftScope = "incognito";

/**
 * The scope that owns the composer currently on screen. Incognito takes over the
 * whole surface, so it wins over the route. Views without a composer still get a
 * distinct key rather than sharing one, so nothing can bleed through them.
 */
export function draftScopeKey(
  route: RouteState,
  incognito: boolean,
): DraftScope {
  if (incognito) return INCOGNITO_DRAFT_SCOPE;
  switch (route.view) {
    case "thread":
      return `thread:${route.threadID}`;
    case "project":
      return `project:${route.projectID}`;
    case "new":
      return "new";
    default:
      return `view:${route.view}`;
  }
}

export function threadDraftScope(threadID: string): DraftScope {
  return `thread:${threadID}`;
}

export function getDraft(
  drafts: ComposerDrafts,
  scope: DraftScope,
): DraftState {
  return drafts[scope] ?? EMPTY_DRAFT;
}

function withDraft(
  drafts: ComposerDrafts,
  scope: DraftScope,
  next: DraftState,
): ComposerDrafts {
  // Prune rather than store an empty draft, so switching through threads does not
  // accumulate a record entry per thread visited.
  if (next.text === "" && next.pastedTexts.length === 0) {
    if (!(scope in drafts)) return drafts;
    const pruned = { ...drafts };
    delete pruned[scope];
    return pruned;
  }
  return { ...drafts, [scope]: next };
}

export function setDraftText(
  drafts: ComposerDrafts,
  scope: DraftScope,
  text: string,
): ComposerDrafts {
  return withDraft(drafts, scope, { ...getDraft(drafts, scope), text });
}

export function setDraftPastedTexts(
  drafts: ComposerDrafts,
  scope: DraftScope,
  pastedTexts: PastedText[],
): ComposerDrafts {
  return withDraft(drafts, scope, { ...getDraft(drafts, scope), pastedTexts });
}

export function setDraft(
  drafts: ComposerDrafts,
  scope: DraftScope,
  next: DraftState,
): ComposerDrafts {
  return withDraft(drafts, scope, next);
}

export function clearDraft(
  drafts: ComposerDrafts,
  scope: DraftScope,
): ComposerDrafts {
  return withDraft(drafts, scope, EMPTY_DRAFT);
}

/** Merge the trimmed draft with its staged pastes into the outgoing content. */
export function composeContent(draft: DraftState): string {
  return [draft.text.trim(), ...draft.pastedTexts.map((pasted) => pasted.text)]
    .filter((part) => part !== "")
    .join("\n\n");
}
