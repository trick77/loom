import { expect, test } from "vitest";
import { buildImageStats } from "./ThreadShell";
import type { Artifact } from "./api";

function artifact(extra: Partial<Artifact>): Artifact {
  return {
    id: "a1",
    displayFilename: "image.png",
    mimeType: "image/png",
    sizeBytes: 1024,
    downloadUrl: "/api/artifacts/a1/download",
    ...extra,
  };
}

test("buildImageStats joins model, resolution and duration", () => {
  const line = buildImageStats(artifact({ model: "flux-pro-1.1", width: 1024, height: 1024, durationMs: 4200 }));
  expect(line).toBe("flux-pro-1.1 · 1024×1024 · 4.2s");
});

test("buildImageStats omits missing or zero segments", () => {
  expect(buildImageStats(artifact({ model: "flux-pro-1.1" }))).toBe("flux-pro-1.1");
  expect(buildImageStats(artifact({ width: 512, height: 0 }))).toBeNull();
  expect(buildImageStats(artifact({ durationMs: 0 }))).toBeNull();
});

test("buildImageStats returns null when no stats are present", () => {
  expect(buildImageStats(artifact({}))).toBeNull();
});
