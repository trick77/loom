import { requestJSON } from "./http";
import type { Usage, UserDirective, UserMemory } from "./types";

export async function getUserMemory(): Promise<UserMemory> {
  return requestJSON(`/api/me/memory`, "failed to load user memory");
}

export async function getUserDirectives(): Promise<UserDirective[]> {
  return requestJSON(`/api/me/directives`, "failed to load user directives");
}

export async function getUsage(): Promise<Usage> {
  return requestJSON(`/api/me/usage`, "failed to load usage");
}
