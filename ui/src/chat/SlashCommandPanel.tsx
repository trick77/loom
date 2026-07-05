import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  AuthExpiredError,
  getMCPServers,
  getMCPTools,
  type MCPServerStatus,
  type MCPToolInfo,
} from "../api";
import i18n from "../i18n";
import { UsagePanel } from "../settings/UsagePanel";
import { Icon } from "./Icon";
import { SLASH_COMMANDS, type SlashCommandName } from "./slashCommands";

/**
 * SlashCommandPanel — the ephemeral overlay a slash command opens. It renders
 * over the current screen (from any surface: start, thread, project, incognito),
 * saves nothing, and closes on Escape / backdrop / the close button.
 */
export function SlashCommandPanel({
  command,
  onClose,
}: {
  command: SlashCommandName;
  onClose(): void;
}) {
  const { t } = useTranslation();
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(0,0,0,0.5)] p-4 backdrop-blur-[2px]"
      role="dialog"
      aria-modal="true"
      aria-label={t(`slash.titles.${command}`)}
      onClick={onClose}
    >
      <div
        className="flex max-h-[80vh] w-full max-w-[760px] flex-col overflow-hidden rounded-2xl border border-[#343432] bg-[#262624] shadow-[0_24px_60px_rgba(0,0,0,0.5)]"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex flex-none items-center justify-between border-b border-[#343432] px-5 py-3.5">
          <div className="flex items-center gap-2 text-sm font-medium text-[#f4f0e8]">
            <span className="font-mono text-[#aaa79e]">/{command}</span>
            <span className="text-[#807d74]">·</span>
            <span>{t(`slash.titles.${command}`)}</span>
          </div>
          <button
            className="grid h-8 w-8 place-items-center rounded-md text-[#aaa79e] hover:bg-[#2a2a28]"
            type="button"
            aria-label={t("common.close")}
            onClick={onClose}
          >
            <Icon name="close" size="18px" />
          </button>
        </div>
        <div className="ui-sidebar-scroll flex-1 overflow-y-auto p-5">
          {command === "mcp" ? (
            <MCPServersView />
          ) : command === "tools" ? (
            <ToolsView />
          ) : command === "usage" ? (
            <UsagePanel />
          ) : (
            <HelpView />
          )}
        </div>
      </div>
    </div>
  );
}

// useAsync loads data once on mount, surfacing loading / error / value so each
// view stays a thin render.
function useAsync<T>(load: () => Promise<T>): { loading: boolean; error: string; value: T | null } {
  const [state, setState] = useState<{ loading: boolean; error: string; value: T | null }>({
    loading: true,
    error: "",
    value: null,
  });
  useEffect(() => {
    let cancelled = false;
    load()
      .then((value) => {
        if (!cancelled) setState({ loading: false, error: "", value });
      })
      .catch((err) => {
        if (cancelled) return;
        const message = err instanceof AuthExpiredError ? i18n.t("slash.sessionExpired") : String(err?.message ?? err);
        setState({ loading: false, error: message, value: null });
      });
    return () => {
      cancelled = true;
    };
    // load is a stable inline closure per view; run exactly once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return state;
}

function PanelState({ loading, error, empty }: { loading: boolean; error: string; empty?: string }) {
  const { t } = useTranslation();
  if (loading) {
    return (
      <div className="flex items-center gap-2 py-6 text-sm text-[#aaa79e]">
        <Icon name="spinner" size="18px" />
        {t("slash.loading")}
      </div>
    );
  }
  if (error !== "") {
    return <div className="py-6 text-sm text-[#e0a89f]">{error}</div>;
  }
  return <div className="py-6 text-sm text-[#aaa79e]">{empty}</div>;
}

function MCPServersView() {
  const { t } = useTranslation();
  const { loading, error, value } = useAsync<MCPServerStatus[]>(getMCPServers);
  if (loading || error !== "" || value === null || value.length === 0) {
    return <PanelState loading={loading} error={error} empty={t("slash.noServers")} />;
  }
  return (
    <table className="w-full border-collapse text-sm">
      <thead>
        <tr className="text-left text-xs uppercase tracking-wide text-[#807d74]">
          <th className="pb-2 pr-4 font-medium">{t("slash.columns.server")}</th>
          <th className="pb-2 pr-4 font-medium">{t("slash.columns.status")}</th>
          <th className="pb-2 pr-4 font-medium">{t("slash.columns.type")}</th>
          <th className="pb-2 pr-4 font-medium">{t("slash.columns.origin")}</th>
          <th className="pb-2 pr-4 font-medium">{t("slash.columns.tools")}</th>
          <th className="pb-2 font-medium">{t("slash.columns.endpoint")}</th>
        </tr>
      </thead>
      <tbody className="text-[#e7e3d9]">
        {value.map((server) => (
          <tr key={server.name} className="border-t border-[#333230] align-top">
            <td className="py-2.5 pr-4 font-medium">{server.name}</td>
            <td className="py-2.5 pr-4">
              <span className="inline-flex items-center gap-1.5">
                <span
                  className={`inline-block h-2 w-2 rounded-full ${server.active ? "bg-[#5fbf7f]" : "bg-[#d1655a]"}`}
                  aria-hidden
                />
                {server.active ? t("slash.up") : t("slash.down")}
              </span>
              {!server.active && server.error !== undefined && server.error !== "" && (
                <div className="mt-1 max-w-[220px] text-xs text-[#c98b82]">{server.error}</div>
              )}
            </td>
            <td className="py-2.5 pr-4 text-[#c9c5bb]">{transportLabel(server.transport)}</td>
            <td className="py-2.5 pr-4 text-[#c9c5bb]">{server.origin}</td>
            <td className="py-2.5 pr-4 tabular-nums text-[#c9c5bb]">{server.toolCount}</td>
            <td className="py-2.5 font-mono text-xs text-[#c9c5bb]">{server.endpoint}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function transportLabel(transport: string): string {
  if (transport === "streamable-http") return "http";
  return transport;
}

function ToolsView() {
  const { t } = useTranslation();
  const { loading, error, value } = useAsync<MCPToolInfo[]>(getMCPTools);
  if (loading || error !== "" || value === null || value.length === 0) {
    return <PanelState loading={loading} error={error} empty={t("slash.noTools")} />;
  }
  // Group by server, preserving each server's tool order.
  const groups = new Map<string, MCPToolInfo[]>();
  for (const tool of value) {
    const key = tool.server === "" ? "other" : tool.server;
    const existing = groups.get(key);
    if (existing) existing.push(tool);
    else groups.set(key, [tool]);
  }
  return (
    <div className="flex flex-col gap-5">
      {[...groups.entries()].map(([server, tools]) => (
        <div key={server}>
          <div className="mb-2 text-xs font-medium uppercase tracking-wide text-[#807d74]">{server}</div>
          <div className="flex flex-col divide-y divide-[#333230] rounded-lg border border-[#333230]">
            {tools.map((tool) => (
              <div key={tool.name} className="px-3.5 py-2.5">
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                  <span className="font-mono text-sm text-[#e7e3d9]">{tool.name}</span>
                  {tool.required !== null && tool.required.length > 0 && (
                    <span className="font-mono text-xs text-[#8f8b81]">({tool.required.join(", ")})</span>
                  )}
                </div>
                {tool.description !== "" && (
                  <div className="mt-1 text-sm text-[#aaa79e]">{tool.description}</div>
                )}
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function HelpView() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col divide-y divide-[#333230] rounded-lg border border-[#333230]">
      {SLASH_COMMANDS.map((command) => (
        <div key={command.name} className="flex items-baseline gap-3 px-3.5 py-2.5">
          <span className="w-24 shrink-0 font-mono text-sm text-[#e7e3d9]">/{command.name}</span>
          <span className="text-sm text-[#aaa79e]">{t(command.description)}</span>
        </div>
      ))}
    </div>
  );
}
