import { requestJSON } from "./http";
import type { Usage, UserDirective, UserFact } from "./types";

export async function getUserFacts(): Promise<UserFact[]> {
  return requestJSON(`/api/me/facts`, "failed to load user facts");
}

export async function getUserDirectives(): Promise<UserDirective[]> {
  return requestJSON(`/api/me/directives`, "failed to load user directives");
}

export async function getUsage(): Promise<Usage> {
  return requestJSON(`/api/me/usage`, "failed to load usage");
}
