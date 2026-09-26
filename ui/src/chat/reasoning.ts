// The only model Loom serves, shown as a static (non-interactive) label in the
// composer — there is no model picker. Display form of Z.ai's glm-5.3-flash,
// reading like a name rather than the model tag.
//
// There is no reasoning-effort selector. The backend picks the level: "high"
// on a normal turn, "low" on helper calls and the forced final answer. The
// model cannot switch thinking off, and leaving the level unset means the
// vendor's "max", which is several times slower.
export const MODEL_LABEL = "GLM 5.3 Flash";
