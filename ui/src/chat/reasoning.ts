// The only model Loom serves, shown as a static (non-interactive) label in the
// composer — there is no model picker. Display form of Xiaomi's
// MiMo-V2.6-Flash (mimo.xiaomi.com), reading like a name rather than the model
// tag.
//
// There is no reasoning-effort selector any more. MiMo's reasoning_effort scale
// was offered here so a user could trade depth for speed per turn, but the
// levels are inert on this family: measured five samples per level, every range
// overlaps every other, and on Flash the three span 17 tokens between them with
// "high" the lowest mean of the three. Loom now omits the parameter rather than
// implying a control that does not exist. Thinking itself is still switched off
// where it should be, by MiMo's own toggle, on the backend.
export const MODEL_LABEL = "MiMo 2.6 Flash";
