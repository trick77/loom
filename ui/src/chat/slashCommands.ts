// Slash commands are local utility commands typed into the composer. They never
// reach the LLM: the composer offers typeahead suggestions, and submitting one
// opens an ephemeral overlay panel (see SlashCommandPanel) instead of sending a
// message. This single registry drives the typeahead, the /help listing, and the
// submit-time dispatch, so the three never drift.

export type SlashCommandName = "mcp" | "tools" | "usage" | "help";

export type SlashCommand = {
  name: SlashCommandName;
  // i18n key for the human-readable description; the `/name` token itself is a
  // typed command and is never translated.
  description: string;
};

export const SLASH_COMMANDS: SlashCommand[] = [
  { name: "mcp", description: "slash.desc.mcp" },
  { name: "tools", description: "slash.desc.tools" },
  { name: "usage", description: "slash.desc.usage" },
  { name: "help", description: "slash.desc.help" },
];

// matchSlashCommand returns the command a draft resolves to, or null. A draft is
// a command only when its trimmed text is exactly "/name" (commands take no
// arguments), so ordinary messages that merely start with "/" are never hijacked.
export function matchSlashCommand(draft: string): SlashCommand | null {
  const trimmed = draft.trim().toLowerCase();
  if (!trimmed.startsWith("/")) return null;
  const name = trimmed.slice(1);
  return SLASH_COMMANDS.find((command) => command.name === name) ?? null;
}

// slashSuggestions returns the commands whose name is prefixed by the draft's
// "/token", for the composer typeahead. It only fires while the draft is a
// single unfinished "/token" (no whitespace yet), so a fully typed message never
// keeps the popover open.
export function slashSuggestions(draft: string): SlashCommand[] {
  if (!/^\/\S*$/.test(draft)) return [];
  const query = draft.slice(1).toLowerCase();
  return SLASH_COMMANDS.filter((command) => command.name.startsWith(query));
}
