import { expectJSON } from "./http";
import type { MCPServerStatus, MCPToolInfo } from "./types";

export async function getMCPServers(): Promise<MCPServerStatus[]> {
  const response = await fetch(`/api/mcp/servers`);
  const body = await expectJSON<{ servers: MCPServerStatus[] }>(response, "failed to load MCP servers");
  return body.servers ?? [];
}

export async function getMCPTools(): Promise<MCPToolInfo[]> {
  const response = await fetch(`/api/mcp/tools`);
  const body = await expectJSON<{ tools: MCPToolInfo[] }>(response, "failed to load MCP tools");
  return body.tools ?? [];
}
