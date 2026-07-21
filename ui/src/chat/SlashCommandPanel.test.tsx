import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "../i18n";
import * as api from "../api";
import { AuthExpiredError } from "../api";
import { SlashCommandPanel } from "./SlashCommandPanel";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return { ...actual, getMCPServers: vi.fn(), getMCPTools: vi.fn() };
});

// The usage view is covered by its own tests; stub it so the panel's routing can
// be asserted without dragging in the usage panel's own data loading.
vi.mock("../settings/UsagePanel", () => ({
  UsagePanel: () => <div data-testid="usage-panel" />,
}));

const getMCPServersMock = vi.mocked(api.getMCPServers);
const getMCPToolsMock = vi.mocked(api.getMCPTools);

function server(
  overrides: Partial<api.MCPServerStatus> = {},
): api.MCPServerStatus {
  return {
    name: "files",
    active: true,
    transport: "stdio",
    endpoint: "stdio://files",
    origin: "built-in",
    toolCount: 3,
    ...overrides,
  };
}

beforeEach(() => {
  getMCPServersMock.mockReset();
  getMCPToolsMock.mockReset();
  getMCPServersMock.mockResolvedValue([]);
  getMCPToolsMock.mockResolvedValue([]);
});

describe("SlashCommandPanel", () => {
  describe("chrome", () => {
    // The body of /usage is stubbed, so the header is the only place its name
    // and title appear — unambiguous queries for the chrome assertions.
    it("renders the command name and its title in the header", () => {
      render(<SlashCommandPanel command="usage" onClose={vi.fn()} />);

      const dialog = screen.getByRole("dialog", { name: "Usage" });
      expect(dialog).toHaveAttribute("aria-modal", "true");
      expect(screen.getByText("/usage")).toBeInTheDocument();
      expect(screen.getByText("Usage")).toBeInTheDocument();
    });

    it("closes on the close button", () => {
      const onClose = vi.fn();
      render(<SlashCommandPanel command="help" onClose={onClose} />);

      fireEvent.click(screen.getByRole("button", { name: "Close" }));

      expect(onClose).toHaveBeenCalledOnce();
    });

    it("closes on Escape and stops listening once unmounted", () => {
      const onClose = vi.fn();
      const { unmount } = render(
        <SlashCommandPanel command="help" onClose={onClose} />,
      );

      fireEvent.keyDown(document.body, { key: "Escape" });
      expect(onClose).toHaveBeenCalledOnce();

      unmount();
      fireEvent.keyDown(document.body, { key: "Escape" });
      expect(onClose).toHaveBeenCalledOnce();
    });

    it("ignores other keys", () => {
      const onClose = vi.fn();
      render(<SlashCommandPanel command="help" onClose={onClose} />);

      fireEvent.keyDown(document.body, { key: "Enter" });

      expect(onClose).not.toHaveBeenCalled();
    });

    it("closes on the backdrop but not on a click inside the panel", () => {
      const onClose = vi.fn();
      render(<SlashCommandPanel command="usage" onClose={onClose} />);

      fireEvent.click(screen.getByText("/usage"));
      expect(onClose).not.toHaveBeenCalled();

      fireEvent.click(screen.getByRole("dialog"));
      expect(onClose).toHaveBeenCalledOnce();
    });
  });

  describe("/help", () => {
    it("lists every slash command with its description", () => {
      render(<SlashCommandPanel command="help" onClose={vi.fn()} />);

      expect(screen.getByText("/mcp")).toBeInTheDocument();
      expect(screen.getByText("/tools")).toBeInTheDocument();
      expect(screen.getByText("/usage")).toBeInTheDocument();
      expect(screen.getByText("MCP server status")).toBeInTheDocument();
      expect(
        screen.getByText("List the available slash commands"),
      ).toBeInTheDocument();
    });
  });

  describe("/usage", () => {
    it("renders the usage panel", () => {
      render(<SlashCommandPanel command="usage" onClose={vi.fn()} />);

      expect(screen.getByTestId("usage-panel")).toBeInTheDocument();
    });
  });

  describe("/mcp", () => {
    it("shows a loading state until the servers resolve", async () => {
      getMCPServersMock.mockReturnValue(new Promise(() => {}));
      render(<SlashCommandPanel command="mcp" onClose={vi.fn()} />);

      expect(screen.getByText("Loading…")).toBeInTheDocument();
    });

    it("shows the empty state when no servers are configured", async () => {
      render(<SlashCommandPanel command="mcp" onClose={vi.fn()} />);

      expect(
        await screen.findByText("No MCP servers configured."),
      ).toBeInTheDocument();
    });

    it("renders a row per server with its status and normalised transport", async () => {
      getMCPServersMock.mockResolvedValue([
        server({
          name: "files",
          transport: "streamable-http",
          endpoint: "http://files",
        }),
        server({
          name: "search",
          active: false,
          transport: "stdio",
          origin: "file",
          toolCount: 0,
          error: "spawn failed",
        }),
      ]);
      render(<SlashCommandPanel command="mcp" onClose={vi.fn()} />);

      expect(await screen.findByText("files")).toBeInTheDocument();
      expect(screen.getByText("search")).toBeInTheDocument();
      // "streamable-http" is displayed as the shorter "http".
      expect(screen.getByText("http")).toBeInTheDocument();
      expect(screen.queryByText("streamable-http")).not.toBeInTheDocument();
      expect(screen.getByText("up")).toBeInTheDocument();
      expect(screen.getByText("down")).toBeInTheDocument();
      expect(screen.getByText("spawn failed")).toBeInTheDocument();
      expect(screen.getByText("http://files")).toBeInTheDocument();
    });

    it("hides the error text for an active server", async () => {
      getMCPServersMock.mockResolvedValue([server({ error: "stale error" })]);
      render(<SlashCommandPanel command="mcp" onClose={vi.fn()} />);

      expect(await screen.findByText("files")).toBeInTheDocument();
      expect(screen.queryByText("stale error")).not.toBeInTheDocument();
    });

    it("surfaces a load failure", async () => {
      getMCPServersMock.mockRejectedValue(new Error("boom"));
      render(<SlashCommandPanel command="mcp" onClose={vi.fn()} />);

      expect(await screen.findByText("boom")).toBeInTheDocument();
    });

    it("shows a session-expired message when auth has lapsed", async () => {
      getMCPServersMock.mockRejectedValue(new AuthExpiredError());
      render(<SlashCommandPanel command="mcp" onClose={vi.fn()} />);

      expect(
        await screen.findByText("Your session expired. Reload to sign in."),
      ).toBeInTheDocument();
    });
  });

  describe("/tools", () => {
    it("shows the empty state when no tools are exposed", async () => {
      render(<SlashCommandPanel command="tools" onClose={vi.fn()} />);

      expect(
        await screen.findByText("No tools are currently exposed."),
      ).toBeInTheDocument();
    });

    it("groups tools by server and renders required args and descriptions", async () => {
      getMCPToolsMock.mockResolvedValue([
        {
          name: "read_file",
          server: "files",
          description: "Read a file",
          required: ["path"],
        },
        { name: "write_file", server: "files", description: "", required: [] },
        {
          name: "ping",
          server: "",
          description: "Ping the host",
          required: null,
        },
      ]);
      render(<SlashCommandPanel command="tools" onClose={vi.fn()} />);

      expect(await screen.findByText("read_file")).toBeInTheDocument();
      expect(screen.getByText("files")).toBeInTheDocument();
      // A tool with a blank server is grouped under "other".
      expect(screen.getByText("other")).toBeInTheDocument();
      expect(screen.getByText("(path)")).toBeInTheDocument();
      expect(screen.getByText("Read a file")).toBeInTheDocument();
      expect(screen.getByText("Ping the host")).toBeInTheDocument();
      // write_file has neither required args nor a description: name only.
      expect(screen.getByText("write_file")).toBeInTheDocument();
    });

    it("surfaces a tools load failure", async () => {
      getMCPToolsMock.mockRejectedValue(new Error("nope"));
      render(<SlashCommandPanel command="tools" onClose={vi.fn()} />);

      expect(await screen.findByText("nope")).toBeInTheDocument();
    });
  });
});
