import { expect, test } from "vitest";
import { ICONS } from "./Icon";

test("copy uses the selected glyph", () => {
  expect(ICONS.copy).toBe("\ue056");
});

test("memory uses the selected glyph", () => {
  expect(ICONS.memory).toBe("\ue055");
});
