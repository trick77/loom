import { expectJSON } from "./http";
import type { Usage, UserDirective, UserMemory } from "./types";

export async function getUserMemory(): Promise<UserMemory> {
  const response = await fetch(`/api/me/memory`);
  return expectJSON<UserMemory>(response, "failed to load user memory");
}

export async function getUserDirectives(): Promise<UserDirective[]> {
  const response = await fetch(`/api/me/directives`);
  return expectJSON<UserDirective[]>(response, "failed to load user directives");
}

export async function getUsage(): Promise<Usage> {
  const response = await fetch(`/api/me/usage`);
  return expectJSON<Usage>(response, "failed to load usage");
}
