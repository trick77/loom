// Package classifier holds the prompt-classifier taxonomy: the fixed set of
// conversation categories, the lean instruction block each one injects into the
// system prompt, and the guidance shown to the model that picks a category.
//
// It is the single source of truth shared by the llm package (which validates the
// category the title call returns) and the httpapi package (which injects the
// matching block). Keep it dependency-free so both can import it without cycles.
//
// Every enrichment block has two parts: (a) FORMAT — how to shape the answer — and
// (b) THINK/SURFACE — how to think about it and what to surface that the user did
// not ask for. Utility categories carry FORMAT only. Blocks stay lean (no filler,
// no hedging) but are complete directives. The 10 categories adapted from
// Perplexity's leaked "Query type specifications" take their FORMAT half from that
// proven source; the THINK/SURFACE half and the Loom-only categories are Loom's.
package classifier

import "strings"

// Category is one conversation classification value. The zero value ("") and any
// unknown string are treated as Unknown — no block is injected.
type Category string

const (
	KnowledgeDiscovery Category = "knowledge_discovery"
	AcademicResearch   Category = "academic_research"
	RecentNews         Category = "recent_news"
	ScienceMath        Category = "science_math"
	Coding             Category = "coding"
	CookingRecipes     Category = "cooking_recipes"
	CreativeWriting    Category = "creative_writing"
	HowTo              Category = "how_to"
	Shopping           Category = "shopping"
	WritingEditing     Category = "writing_editing"
	Summarization      Category = "summarization"
	Brainstorming      Category = "brainstorming"
	Planning           Category = "planning"
	Weather            Category = "weather"
	Translation        Category = "translation"
	URLLookup          Category = "url_lookup"
	// ImageGeneration labels threads routed to image generation. It is a hidden
	// category: assigned deterministically by the request heuristics
	// (imageArtifactRequired), never offered to or chosen by the classifying model.
	// It injects no block — the image path ignores the classifier block anyway.
	ImageGeneration Category = "image_generation"
	// General is the fallback: anything that does not clearly fit another
	// category, including chit-chat and personal conversation. It injects a lean
	// prose-default directive (no FORMAT/THINK structure).
	General Category = "general"
)

// entry holds the per-category metadata: the short gloss shown to the classifying
// model and the instruction block injected into the system prompt for that
// category. An empty block means "inject nothing". A hidden entry is a valid
// category assigned by other code paths (e.g. request heuristics) but never
// offered to the classifying model — PromptGuide omits it.
type entry struct {
	gloss  string
	block  string
	hidden bool
}

// catalog is the ordered taxonomy. Order is the order categories are presented to
// the classifying model and must stay stable.
var catalog = []struct {
	cat   Category
	entry entry
}{
	{KnowledgeDiscovery, entry{
		gloss: "explaining or learning about a broad topic, concept, or person that fits no more specific category",
		block: "Answer in flowing prose, not bullet or numbered lists unless the content is a true enumeration (steps, parameters, a checklist). If the query is about a person, give a short comprehensive biography; if multiple people match, describe each individually and never mix their information. When the topic has real depth worth exploring, end with a section of 1-3 recent developments tailored to the angle revealed by the query — historical, practical, or theoretical. Skip it for simple factual lookups that a sentence or two fully answers.",
	}},
	{AcademicResearch, entry{
		gloss: "scholarly, paper-oriented, rigorous research questions",
		block: "Give a long, detailed answer as a scientific write-up using markdown sections and headings, in prose, not bullet or numbered lists unless the content is a true enumeration (steps, parameters, a checklist). Name the seminal and most recent work and the key researchers, surface open debates and competing methodologies, and flag the strength of the evidence behind each claim.",
	}},
	{RecentNews, entry{
		gloss: "current events, latest news and happenings",
		block: "Summarize concisely, group by topic, and use lists with the news titles highlighted; select diverse perspectives, prioritize trustworthy sources, and prioritize recent events. When the user's location is known, lead with news relevant to their country or region before global items, without omitting major world news. Close with the ongoing threads worth watching next.",
	}},
	{ScienceMath, entry{
		gloss: "calculations or math/physics/science problems to solve",
		block: "For a simple calculation, give only the final result. Otherwise show the working steps and wrap all math in LaTeX, then add the intuition behind the result and flag a common pitfall or misconception.",
	}},
	{Coding, entry{
		gloss: "programming, code, debugging, software questions",
		block: "Put code in fenced markdown blocks tagged with the language; present the code first, then explain it. Call out edge cases, gotchas, and version-specific caveats, and offer one alternative approach when relevant.",
	}},
	{CookingRecipes, entry{
		gloss: "recipes and cooking instructions",
		block: "List ingredients as a bullet list with exact amounts, then give numbered step-by-step instructions with precise detail at each step. Note useful substitutions.",
	}},
	{CreativeWriting, entry{
		gloss: "stories, poems, fiction, lyrics, other creative writing",
		block: "Follow the user's instructions precisely; do not use or cite search results.",
	}},
	{HowTo, entry{
		gloss: "practical, real-world step-by-step how-to tasks (not programming)",
		block: "Give numbered steps; list any required tools or materials as a bullet list first. Flag the single most common mistake people make.",
	}},
	{Shopping, entry{
		gloss: "product recommendations, comparisons, what to buy",
		block: "Lay out the comparison criteria as a table or list. Name credible alternatives and flag what to watch out for before buying. When the user's location is known, favor options available in their region and note local pricing or retailers where it matters.",
	}},
	{WritingEditing, entry{
		gloss: "improving, proofreading, or rewriting the user's own text",
		block: "Return the revised text. Briefly note the key changes you made.",
	}},
	{Summarization, entry{
		gloss: "summarizing or condensing provided material",
		block: "Give a tight summary; use a list when there are multiple distinct points. Add the one thing the source leaves out, or the key tension within it.",
	}},
	{Brainstorming, entry{
		gloss: "generating ideas, names, or options",
		block: "Give at most 12 options. Recommend one, and add a non-obvious angle.",
	}},
	{Planning, entry{
		gloss: "trips, schedules, itineraries, multi-step plans",
		block: "Give a structured plan, numbered or grouped into phases. Call out a thing people commonly forget, and include one contingency.",
	}},
	{Weather, entry{
		gloss: "weather forecasts",
		block: "Keep it very short — give only the forecast, or say so if the data isn't available. When the user asks without naming a place and their location is known, use it.",
	}},
	{Translation, entry{
		gloss: "translating text between languages",
		block: "Provide only the translation. Do not cite sources or add notes.",
	}},
	{URLLookup, entry{
		gloss: "reading or answering about a specific URL the user gave",
		block: "Answer relying solely on the content of the page at the URL, and cite it.",
	}},
	{ImageGeneration, entry{
		gloss:  "image or visual-artifact generation (assigned by request heuristics, never chosen by this classifier)",
		block:  "",
		hidden: true,
	}},
	{General, entry{
		gloss: "anything else, including chit-chat and personal conversation",
		block: "Answer in prose, not bullet or numbered lists unless the content is a true enumeration (steps, parameters, a checklist).",
	}},
}

// byCategory indexes the catalog for O(1) lookups.
var byCategory = func() map[Category]entry {
	m := make(map[Category]entry, len(catalog))
	for _, c := range catalog {
		m[c.cat] = c.entry
	}
	return m
}()

// Valid reports whether s is one of the known category values.
func Valid(s string) bool {
	_, ok := byCategory[Category(s)]
	return ok
}

// Normalize returns the given value as a known Category, or General when it is
// empty or unrecognized. Use it to coerce model output before storing.
func Normalize(s string) Category {
	if Valid(s) {
		return Category(s)
	}
	return General
}

// Block returns the system-prompt instruction block for a category, or "" when
// the category is empty or unknown (inject nothing).
func Block(s string) string {
	return byCategory[Category(s)].block
}

// Match extracts a category from a classifying model's free-form reply. It is more
// tolerant than Normalize: it lowercases and scans the reply token by token (on
// non [a-z_] boundaries) and returns the first token that is a known category, so
// replies like `coding`, `"coding"`, `coding.`, or `The category is coding` all
// resolve correctly. Anything with no recognizable category — including an empty
// reply — yields General.
func Match(reply string) Category {
	for _, tok := range strings.FieldsFunc(strings.ToLower(reply), func(r rune) bool {
		return (r < 'a' || r > 'z') && r != '_'
	}) {
		if Valid(tok) {
			return Category(tok)
		}
	}
	return General
}

// Values returns every category value in catalog order, including General.
func Values() []Category {
	out := make([]Category, 0, len(catalog))
	for _, c := range catalog {
		out = append(out, c.cat)
	}
	return out
}

// PromptGuide renders the taxonomy as a newline-separated list of
// "- value: gloss" lines, for embedding in the classifying model's system prompt.
// Hidden categories are omitted so the model is never offered them.
func PromptGuide() string {
	var b strings.Builder
	first := true
	for _, c := range catalog {
		if c.entry.hidden {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString("- ")
		b.WriteString(string(c.cat))
		b.WriteString(": ")
		b.WriteString(c.entry.gloss)
	}
	return b.String()
}
