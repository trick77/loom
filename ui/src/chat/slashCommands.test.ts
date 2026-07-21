import { describe, expect, it } from "vitest";

import {
  matchSlashCommand,
  slashSuggestions,
  SLASH_COMMANDS,
} from "./slashCommands";

describe("matchSlashCommand", () => {
  it("resolves an exact /name to its command", () => {
    expect(matchSlashCommand("/mcp")?.name).toBe("mcp");
    expect(matchSlashCommand("/tools")?.name).toBe("tools");
    expect(matchSlashCommand("/usage")?.name).toBe("usage");
    expect(matchSlashCommand("/help")?.name).toBe("help");
  });

  it("is case-insensitive and tolerates surrounding whitespace", () => {
    expect(matchSlashCommand("/MCP")?.name).toBe("mcp");
    expect(matchSlashCommand("  /Help  ")?.name).toBe("help");
  });

  it("does not match a command with trailing arguments (commands take none)", () => {
    // Critical invariant: anything that isn't an exact command must fall through
    // to a normal LLM message, never be hijacked as a command.
    expect(matchSlashCommand("/mcp now")).toBeNull();
    expect(matchSlashCommand("/help me write a poem")).toBeNull();
  });

  it("does not match unknown or partial commands", () => {
    expect(matchSlashCommand("/mcpx")).toBeNull();
    expect(matchSlashCommand("/m")).toBeNull();
    expect(matchSlashCommand("/")).toBeNull();
  });

  it("does not match ordinary text or a leading slash in prose", () => {
    expect(matchSlashCommand("hello")).toBeNull();
    expect(matchSlashCommand("and/or")).toBeNull();
    expect(matchSlashCommand("/path/to/file")).toBeNull();
  });
});

describe("slashSuggestions", () => {
  it("lists all commands for a lone slash", () => {
    expect(slashSuggestions("/").map((c) => c.name)).toEqual(
      SLASH_COMMANDS.map((c) => c.name),
    );
  });

  it("prefix-filters by the typed token, case-insensitively", () => {
    expect(slashSuggestions("/m").map((c) => c.name)).toEqual(["mcp"]);
    expect(slashSuggestions("/T").map((c) => c.name)).toEqual(["tools"]);
    expect(slashSuggestions("/help").map((c) => c.name)).toEqual(["help"]);
  });

  it("returns nothing once the draft contains whitespace or a newline", () => {
    // A finished message that merely starts with "/" must not keep the popover open.
    expect(slashSuggestions("/mcp ")).toEqual([]);
    expect(slashSuggestions("/help me")).toEqual([]);
    expect(slashSuggestions("/mcp\n")).toEqual([]);
  });

  it("returns nothing for a non-slash draft or an unmatched prefix", () => {
    expect(slashSuggestions("hello")).toEqual([]);
    expect(slashSuggestions("/zzz")).toEqual([]);
  });
});
