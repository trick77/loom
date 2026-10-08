import { requestJSON } from "./http";
import type { MCPServerStatus, MCPToolInfo } from "./types";

export async function getMCPServers(): Promise<MCPServerStatus[]> {
  const body = await requestJSON<{ servers: MCPServerStatus[] }>(
    `/api/mcp/servers`,
    "failed to load MCP servers",
  );
  return body.servers ?? [];
}

export async function getMCPTools(): Promise<MCPToolInfo[]> {
  const body = await requestJSON<{ tools: MCPToolInfo[] }>(
    `/api/mcp/tools`,
    "failed to load MCP tools",
  );
  return body.tools ?? [];
}
