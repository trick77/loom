package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/llmwire"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/store"
)

// TestEval_UserMemoryReplay replays a real message history day by day through
// the old text-blob user memory (with the truncation guards) and the facts
// design on a live model, then measures what survived. Live and costly: it
// runs only with LOOM_EVAL=1.
//
//	LOOM_EVAL=1 LOOM_EVAL_HISTORY=history.json LOOM_EVAL_MODEL=<model id> \
//	LOOM_EVAL_OUT=report.json LLMWIRE_<PROVIDER>_API_KEY=... \
//	go test -run Eval_UserMemoryReplay -timeout 8h -v ./internal/httpapi/
//
// The history is a JSON array of {id, thread_id, role, content, created_at}
// (sqlite3 -json over messages). LOOM_EVAL_MAX_DAYS (default 330) refuses a
// longer history so its cost is estimated first.
func TestEval_UserMemoryReplay(t *testing.T) {
	if os.Getenv("LOOM_EVAL") != "1" {
		t.Skip("live eval: set LOOM_EVAL=1")
	}
	model := os.Getenv("LOOM_EVAL_MODEL")
	if model == "" {
		t.Fatal("LOOM_EVAL_MODEL is required: the llmwire model id to evaluate")
	}
	history := loadEvalHistory(t, os.Getenv("LOOM_EVAL_HISTORY"))
	days := groupByDay(history)
	maxDays, _ := strconv.Atoi(envOr("LOOM_EVAL_MAX_DAYS", "330"))
	t.Logf("history: %d messages over %d active days (%s .. %s)", len(history), len(days), days[0].day, days[len(days)-1].day)
	if len(days) > maxDays {
		t.Fatalf("%d active days > LOOM_EVAL_MAX_DAYS %d: estimate the cost first", len(days), maxDays)
	}

	ctx := context.Background()
	client, err := llm.NewClient(llm.Config{Models: llm.Roles{Chat: model}}, nil)
	if err != nil {
		t.Fatalf("llm client: %v", err)
	}
	judge, err := llmwire.FromEnvModels(llmwire.Config{}, model)
	if err != nil {
		t.Fatalf("judge client: %v", err)
	}
	db, st, user := evalStore(t, history)
	rec := &recordingFacts{Client: client}
	s := &server{thread: st, llm: rec}

	r := evalReport{Model: model, Days: len(days), Messages: len(history), FinalBuckets: map[string]int{}}
	probeDay := max(len(days)*15/100, 1)
	forgetDay := len(days) / 2
	var blob string
	var probes []chat.UserFact
	var forgotten []chat.UserFact

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the old design, independent of the facts run
		defer wg.Done()
		blob = replayBlob(ctx, t, client, days, &r)
	}()

	for i, d := range days {
		if i == forgetDay {
			forgotten, d.messages = forgetOneFact(ctx, t, db, st, user, d)
		}
		before, _ := st.ListActiveUserFacts(ctx, user.ID)
		rec.reset()
		for rest := d.messages; len(rest) > 0; {
			_, used := factsTranscriptUsed(rest)
			if _, _, err := s.applyUserFactsBatch(ctx, user, rest[:used]); err != nil {
				r.FactErrors++
				t.Logf("day %s: facts batch failed: %v", d.day, err)
			}
			rest = rest[used:]
		}
		r.addRecall(ctx, t, judge, model, d, before, rec.touched())
		if (i+1)%7 == 0 {
			if err := s.consolidateUserFactsIfDue(ctx, user, chat.UserFactCursor{}); err != nil {
				t.Logf("day %s: consolidation failed: %v", d.day, err)
			}
		}
		if i+1 == probeDay {
			probes = pickProbes(ctx, t, st, user.ID, 20)
			t.Logf("probes picked after day %d: %d facts", probeDay, len(probes))
		}
		if (i+1)%25 == 0 {
			t.Logf("facts: day %d/%d", i+1, len(days))
		}
	}
	wg.Wait()

	final, _ := st.ListActiveUserFacts(ctx, user.ID)
	end := days[len(days)-1].end
	r.FinalFacts = len(final)
	for _, f := range final {
		r.FinalBuckets[string(f.Bucket(end))]++
	}
	r.BlobChars = len([]rune(blob))
	factsText := renderUserFacts(final, end)
	// A forgotten probe is gone on purpose: it is checked on its own below.
	probes = slices.DeleteFunc(probes, func(p chat.UserFact) bool {
		return slices.ContainsFunc(forgotten, func(f chat.UserFact) bool { return f.ID == p.ID })
	})
	r.ProbeCount = len(probes)
	r.FactsSurvival = survival(ctx, t, judge, model, probes, factsText, survivalTask)
	r.BlobSurvival = survival(ctx, t, judge, model, probes, blob, survivalTask)
	if len(forgotten) > 0 {
		r.ForgottenFact = forgotten[0].Content
		r.ForgottenBack = survival(ctx, t, judge, model, forgotten, factsText, forgottenTask) > 0
	}
	r.write(t)

	if r.FactsSurvival < 0.9 {
		t.Errorf("facts survival %.0f%% < 90%%", 100*r.FactsSurvival)
	}
	if r.FactsSurvival <= r.BlobSurvival {
		t.Errorf("facts survival %.0f%% does not beat the blob's %.0f%%", 100*r.FactsSurvival, 100*r.BlobSurvival)
	}
	if recall := r.recall(); recall < 0.8 {
		t.Errorf("reinforce recall %.0f%% < 80%%: consider the code-side reinforce fallback", 100*recall)
	}
}

type evalMessage struct {
	ID        string `json:"id"`
	ThreadID  string `json:"thread_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

type evalDay struct {
	day      string
	end      time.Time
	messages []chat.Message
}

type evalReport struct {
	Model         string         `json:"model"`
	Days          int            `json:"days"`
	Messages      int            `json:"messages"`
	FactErrors    int            `json:"fact_errors"`
	BlobErrors    int            `json:"blob_errors"`
	BlobRefusals  int            `json:"blob_refusals"`
	FinalFacts    int            `json:"final_facts"`
	FinalBuckets  map[string]int `json:"final_buckets"`
	BlobChars     int            `json:"blob_chars"`
	ProbeCount    int            `json:"probes"`
	FactsSurvival float64        `json:"facts_survival"`
	BlobSurvival  float64        `json:"blob_survival"`
	Touched       int            `json:"reinforce_touched"`
	Missed        int            `json:"reinforce_missed"`
	ForgottenFact string         `json:"forgotten_fact"`
	ForgottenBack bool           `json:"forgotten_came_back"`
	mu            sync.Mutex
}

func (r *evalReport) recall() float64 {
	if r.Touched+r.Missed == 0 {
		return 1
	}
	return float64(r.Touched) / float64(r.Touched+r.Missed)
}

func (r *evalReport) write(t *testing.T) {
	data, _ := json.MarshalIndent(r, "", "  ")
	t.Logf("report:\n%s\nreinforce recall: %.0f%%", data, 100*r.recall())
	if out := os.Getenv("LOOM_EVAL_OUT"); out != "" {
		if err := os.WriteFile(out, data, 0o600); err != nil {
			t.Errorf("write report: %v", err)
		}
	}
}

// addRecall asks the judge which facts the day's messages mention that the
// refresh left untouched: reinforce recall = touched / (touched + missed).
func (r *evalReport) addRecall(ctx context.Context, t *testing.T, judge *llmwire.Client, model string, d evalDay, before []chat.UserFact, touched map[string]bool) {
	var untouched []chat.UserFact
	for _, f := range before {
		if !touched[f.ID] {
			untouched = append(untouched, f)
		}
	}
	missed := 0
	if len(untouched) > 0 {
		transcript, _, _ := factsTranscript(d.messages)
		ids, err := judgeIDs(ctx, judge, model,
			"Which of these facts about the user do the messages below mention, rely on, or confirm again (even in passing)? Output ONLY {\"ids\":[...]}.",
			factList(untouched)+"\nMessages:\n"+transcript)
		if err != nil {
			t.Logf("day %s: recall judge failed: %v", d.day, err)
		}
		missed = len(ids)
	}
	inBefore := 0
	for _, f := range before {
		if touched[f.ID] {
			inBefore++
		}
	}
	r.mu.Lock()
	r.Touched += inBefore
	r.Missed += missed
	r.mu.Unlock()
}

const (
	// survivalTask counts a fact as kept when its information is still there,
	// reworded or updated to a newer value.
	survivalTask = "For each fact, is it still present in the memory below (same information, possibly reworded or updated to a newer value)? Output ONLY {\"ids\":[...]} with the ids that are present."
	// forgottenTask is stricter: a newer replacement is not the forgotten fact.
	forgottenTask = "For each fact, does the memory below still state THIS specific fact (reworded counts; a different, newer value that replaced it does not)? Output ONLY {\"ids\":[...]} with the ids still stated."
)

// survival is the share of probes the judge finds in memory text.
func survival(ctx context.Context, t *testing.T, judge *llmwire.Client, model string, probes []chat.UserFact, memory, task string) float64 {
	if len(probes) == 0 {
		return 0
	}
	ids, err := judgeIDs(ctx, judge, model, task,
		factList(probes)+"\nMemory:\n\"\"\"\n"+memory+"\n\"\"\"")
	if err != nil {
		t.Errorf("survival judge failed: %v", err)
	}
	return float64(len(ids)) / float64(len(probes))
}

func judgeIDs(ctx context.Context, judge *llmwire.Client, model, task, input string) ([]string, error) {
	answer := 2000
	resp, _, err := judge.Chat(ctx, llmwire.ChatRequest{
		Model: model,
		Messages: []llmwire.Message{
			llmwire.TextMessage(llmwire.RoleSystem, "You are a careful evaluator. "+task),
			llmwire.TextMessage(llmwire.RoleUser, input),
		},
		Reasoning:       llmwire.ReasoningBalanced(),
		MaxAnswerTokens: &answer,
	})
	if err != nil {
		return nil, err
	}
	raw, ok := llmwire.JSONObject(resp.Content)
	if !ok {
		return nil, fmt.Errorf("judge reply has no JSON: %.200q", resp.Content)
	}
	var out struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out.IDs, nil
}

func factList(facts []chat.UserFact) string {
	var b strings.Builder
	b.WriteString("Facts:\n")
	for _, f := range facts {
		fmt.Fprintf(&b, "- [%s] %s\n", f.ID, f.Content)
	}
	return b.String()
}

// pickProbes takes up to n early facts, long-term ones first.
func pickProbes(ctx context.Context, t *testing.T, st *chat.Store, userID string, n int) []chat.UserFact {
	facts, err := st.ListActiveUserFacts(ctx, userID)
	if err != nil {
		t.Fatalf("probes: %v", err)
	}
	slices.SortStableFunc(facts, func(a, b chat.UserFact) int {
		if a.Durable != b.Durable {
			if a.Durable {
				return -1
			}
			return 1
		}
		return a.FirstSeen.Compare(b.FirstSeen)
	})
	return facts[:min(n, len(facts))]
}

// forgetOneFact forgets the newest long-term fact (or any) and adds the user's
// forget request to the day's messages, as the tool path would see it.
func forgetOneFact(ctx context.Context, t *testing.T, db *sql.DB, st *chat.Store, user auth.User, d evalDay) ([]chat.UserFact, []chat.Message) {
	facts, _ := st.ListActiveUserFacts(ctx, user.ID)
	if len(facts) == 0 || len(d.messages) == 0 {
		return nil, d.messages
	}
	target := facts[0]
	for _, f := range facts {
		if f.Durable {
			target = f
			break
		}
	}
	forgotten, err := st.ForgetUserFacts(ctx, user.ID, []string{target.ID})
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	m := d.messages[0]
	req := chat.Message{ID: "eval-forget", ThreadID: m.ThreadID, Role: chat.RoleUser, Content: "Please forget this about me: " + target.Content, CreatedAt: m.CreatedAt}
	insertEvalMessage(t, db, user.ID, req)
	d.messages = append([]chat.Message{req}, d.messages...)
	return forgotten, d.messages
}

// replayBlob runs the old design: one blob rewritten per day, with today's
// guards (a truncated reply or a result under half the prior is refused).
func replayBlob(ctx context.Context, t *testing.T, client *llm.Client, days []evalDay, r *evalReport) string {
	prompt := legacyUserMemoryPrompt(chat.MaxUserMemoryLength)
	blob := ""
	for i, d := range days {
		msgs := d.messages
		if len(msgs) > memoryRebuildLimit {
			msgs = msgs[len(msgs)-memoryRebuildLimit:]
		}
		transcript := transcriptFromMessages(msgs)
		if strings.TrimSpace(transcript) == "" {
			continue
		}
		next, err := client.GenerateMemory(ctx, "", blob, transcript, "", prompt, "")
		switch {
		case err != nil:
			r.mu.Lock()
			r.BlobErrors++
			r.mu.Unlock()
		case blob != "" && 2*len([]rune(next)) < len([]rune(blob)):
			r.mu.Lock()
			r.BlobRefusals++
			r.mu.Unlock()
		case strings.TrimSpace(next) != "":
			if runes := []rune(next); len(runes) > chat.MaxUserMemoryLength {
				next = string(runes[:chat.MaxUserMemoryLength])
			}
			blob = next
		}
		if (i+1)%25 == 0 {
			t.Logf("blob: day %d/%d", i+1, len(days))
		}
	}
	return blob
}

// recordingFacts records which facts each refresh touched.
type recordingFacts struct {
	*llm.Client
	mu  sync.Mutex
	ids map[string]bool
}

func (c *recordingFacts) reset() {
	c.mu.Lock()
	c.ids = map[string]bool{}
	c.mu.Unlock()
}

func (c *recordingFacts) touched() map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ids
}

func (c *recordingFacts) ExtractUserFacts(ctx context.Context, facts []llm.FactInput, transcript, excluded string, forgotten []string, lang string) ([]llm.FactOp, error) {
	ops, err := c.Client.ExtractUserFacts(ctx, facts, transcript, excluded, forgotten, lang)
	c.mu.Lock()
	for _, op := range ops {
		if op.Op == "update" || op.Op == "reinforce" {
			c.ids[op.ID] = true
		}
	}
	c.mu.Unlock()
	return ops, err
}

func factsTranscriptUsed(messages []chat.Message) (string, int) {
	transcript, _, used := factsTranscript(messages[:min(len(messages), chat.UserFactBatchLimit)])
	return transcript, used
}

func loadEvalHistory(t *testing.T, path string) []chat.Message {
	if path == "" {
		t.Fatal("LOOM_EVAL_HISTORY is required")
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var rows []evalMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	out := make([]chat.Message, 0, len(rows))
	for _, row := range rows {
		at, err := time.ParseInLocation(time.DateTime, row.CreatedAt, time.UTC)
		if err != nil {
			t.Fatalf("message %s: created_at %q: %v", row.ID, row.CreatedAt, err)
		}
		out = append(out, chat.Message{ID: row.ID, ThreadID: row.ThreadID, Role: chat.Role(row.Role), Content: row.Content, CreatedAt: at})
	}
	slices.SortStableFunc(out, func(a, b chat.Message) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func groupByDay(messages []chat.Message) []evalDay {
	var days []evalDay
	for _, m := range messages {
		day := m.CreatedAt.Format(time.DateOnly)
		if len(days) == 0 || days[len(days)-1].day != day {
			days = append(days, evalDay{day: day})
		}
		d := &days[len(days)-1]
		d.messages = append(d.messages, m)
		d.end = m.CreatedAt
	}
	return days
}

// evalStore loads the history into a fresh database: one user, its threads,
// its messages with their original dates.
func evalStore(t *testing.T, history []chat.Message) (*sql.DB, *chat.Store, auth.User) {
	db, err := store.Open(filepath.Join(t.TempDir(), "eval.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	user := auth.User{ID: "eval-user", Username: "eval"}
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES (?, ?, ?, 'user')`, user.ID, "eval", "eval"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	threads := map[string]bool{}
	for _, m := range history {
		if !threads[m.ThreadID] {
			threads[m.ThreadID] = true
			if _, err := db.Exec(`INSERT INTO threads (id, user_id, title, created_at) VALUES (?, ?, 'eval', ?)`,
				m.ThreadID, user.ID, m.CreatedAt.Format(time.DateTime)); err != nil {
				t.Fatalf("insert thread: %v", err)
			}
		}
		insertEvalMessage(t, db, user.ID, m)
	}
	return db, chat.NewStore(db), user
}

func insertEvalMessage(t *testing.T, db *sql.DB, userID string, m chat.Message) {
	if _, err := db.Exec(`INSERT INTO messages (id, thread_id, user_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, m.ThreadID, userID, string(m.Role), m.Content, m.CreatedAt.Format(time.DateTime)); err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// legacyUserMemoryPrompt is the old blob prompt, kept here only as the eval's
// baseline.
func legacyUserMemoryPrompt(budget int) string {
	sections := []struct {
		heading string
		pct     int
		note    string
	}{
		{"## Work context", 12, "the user's professional life: employer/org, role, field, what they build or work on, tools and stack, and hard work constraints"},
		{"## Personal context", 11, "durable personal identity and TASTE facts: where they live, languages, family/relationships mentioned in passing, hobbies, and likes/dislikes as personal taste (food, media, places, activities) — NOT preferences about how the assistant should respond, which are the user's separate standing instructions"},
		{"## Top of mind", 13, "what the user is actively focused on right now: current projects, open goals, decisions in flight, recurring recent themes. CHURNS: drop items once they look finished, superseded, or stop coming up"},
		{"### Recent months", 36, "the richest, most detailed section: notable threads of work and life from roughly the last few months"},
		{"### Earlier context", 18, "older but still relevant background that has faded from active focus"},
		{"### Long-term background", 10, "durable, long-settled facts and milestones worth keeping indefinitely, compressed to essentials"},
	}
	var b strings.Builder
	b.WriteString("You maintain a compact, durable memory about the user so the assistant can stay personalized across all of their chats. ")
	b.WriteString("Given the existing memory and recent conversation, output an UPDATED memory using exactly these markdown headings, each followed by terse '- ' fragment lines. Use these headings and no others, and omit any heading that would have no items:\n")
	briefHistoryWritten := false
	for _, s := range sections {
		if strings.HasPrefix(s.heading, "### ") && !briefHistoryWritten {
			b.WriteString("## Brief history — the user's story over time, most recent first, split into the three sub-sections below. As items age and stop recurring, DEMOTE them one level on each regeneration: Top of mind → Recent months → Earlier context → Long-term background (or drop when no longer worth keeping); promote back up only when an item becomes active again. You are given no real dates — judge recency by how prominently and recently each item appears in the conversation and prior memory, not by calendar math. Brief history as a whole should be the largest part of the memory.\n")
			briefHistoryWritten = true
		}
		fmt.Fprintf(&b, "%s — %s. Aim for roughly %d characters.\n", s.heading, s.note, budget*s.pct/100)
	}
	fmt.Fprintf(&b, "The character targets are SOFT proportions of the memory's budget, not hard limits: let stronger sections run longer and weaker ones shorter, but keep the WHOLE memory under %d characters.\n", budget)
	b.WriteString("Record likes and dislikes only as personal taste under ## Personal context; never record instructions about how to respond (answer length, detail, tone, format, language, forms of address) — those are the user's standing instructions, a separate layer.\n")
	b.WriteString("Do NOT start facts with \"The user\" or similar filler subjects; drop filler words when meaning stays clear. ")
	b.WriteString("Write in a terse, telegraphic \"caveman\" style: keep only load-bearing words; drop articles (a/the), pronouns, and linking verbs (e.g. \"Lives Zurich; backend dev at Acme\"). ")
	b.WriteString("Drop chit-chat, one-off task details, and anything about other people except as it bears on the user. Replace outdated facts with their newest value instead of listing both. ")
	b.WriteString("NEVER store passwords, API keys, secrets, payment details, or other sensitive credentials. ")
	b.WriteString("Output ONLY the memory content, no preamble.")
	return b.String()
}
