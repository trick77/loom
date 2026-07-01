// Barrel for the API client. The client is split into one module per domain
// (auth, projects, threads, artifacts, documents, sharing, stream, user) plus
// shared types (./types) and a shared fetch helper (./http). This barrel
// re-exports the full public surface so existing `import { ... } from "../api"`
// call sites keep working unchanged.
export * from "./types";
export { AuthExpiredError } from "./http";
export * from "./auth";
export * from "./projects";
export * from "./threads";
export * from "./artifacts";
export * from "./documents";
export * from "./sharing";
export * from "./stream";
export * from "./user";
