package turn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/llm"
)

// TestDiagramStyleEval is the measurement behind diagramStyle: fixed prompts
// against the configured chat model, half that should draw and half that
// should not (everyday questions, a chart of numbers, an image, plain steps).
// It calls a real model, so it is skipped unless LOOM_DIAGRAM_EVAL_DIR is set
// (CI has none). The model and key come from the documented environment:
//
//	LOOM_DIAGRAM_EVAL_DIR=/tmp/loom-diagram-eval BACKEND_CHAT_MODEL=... \
//	LLMWIRE_<PROVIDER>_API_KEY=... go test ./internal/turn/ -run DiagramStyleEval -v
//
// Every answer lands in the directory as <case>.txt, and the counts in
// summary.txt. Whether mermaid parses the fences is the UI's half: run
// ui/src/chat/diagramEval.test.ts with the same variable. An answer off its
// expectation is counted, not failed: the numbers are the result. The turn
// runs without tools, so the image case can only decline or describe.
func TestDiagramStyleEval(t *testing.T) {
	dir := os.Getenv("LOOM_DIAGRAM_EVAL_DIR")
	if dir == "" {
		t.Skip("set LOOM_DIAGRAM_EVAL_DIR (and BACKEND_CHAT_MODEL with its key) to run the live diagram eval")
	}
	model := os.Getenv("BACKEND_CHAT_MODEL")
	if model == "" {
		t.Fatal("BACKEND_CHAT_MODEL is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A run starts on an empty slate: an answer left from an earlier run, for
	// a case that errs or is skipped now, would be read by the UI half as this
	// run's. Only the .txt files the eval writes go; nothing else in dir.
	old, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	// The answer budget a chat turn gets (config's default), so a long answer
	// is cut where the app cuts it.
	client, err := llm.NewClient(llm.Config{Models: llm.Roles{Chat: model}, MaxCompletionTokens: 16384, Timeout: 3 * time.Minute}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	system := loomSystemPrompt + languageDirective(auth.User{})
	// LOOM_DIAGRAM_EVAL_TOKEN_CAP stops the run once that many completion
	// tokens are spent: a prompt not yet sent is skipped, not answered.
	tokenCap, _ := strconv.Atoi(os.Getenv("LOOM_DIAGRAM_EVAL_TOKEN_CAP"))

	// Four at a time; each writes only its own slot of results.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		spent   llm.TokenUsage
		results = make([]diagramEvalResult, len(diagramEvalCases))
		slots   = make(chan struct{}, 4)
	)
	for i, c := range diagramEvalCases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			mu.Lock()
			over := tokenCap > 0 && spent.CompletionTokens >= tokenCap
			mu.Unlock()
			if over {
				results[i] = diagramEvalResult{c: c, err: fmt.Errorf("skipped: completion token cap %d reached", tokenCap)}
				return
			}
			res, err := client.StreamChatResult(context.Background(), []llm.Message{
				{Role: "system", Content: system},
				{Role: "user", Content: c.prompt},
			}, nil)
			mu.Lock()
			spent.PromptTokens += res.Usage.PromptTokens
			spent.CompletionTokens += res.Usage.CompletionTokens
			mu.Unlock()
			results[i] = diagramEvalResult{c: c, answer: res.Content, err: err}
		}()
	}
	wg.Wait()

	var drawWanted, drew, unasked, failed, outsideFence int
	var lines []string
	for _, r := range results {
		if r.err != nil {
			failed++
			lines = append(lines, fmt.Sprintf("ERROR %s: %v", r.c.name, r.err))
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, r.c.name+".txt"), []byte(r.answer), 0o644); err != nil {
			t.Fatal(err)
		}
		fences := strings.Count(r.answer, "```mermaid")
		// A diagram written without its fence, or under another tag, is
		// printed as text: the reader gets no picture.
		if fences == 0 && diagramWithoutFence(r.answer) {
			outsideFence++
		}
		mark := "ok"
		switch {
		case r.c.draw && fences == 0:
			mark = "MISSING"
		case !r.c.draw && fences > 0:
			mark = "UNASKED"
			unasked++
		case fences > 1:
			mark = "SEVERAL"
		}
		if r.c.draw {
			drawWanted++
			if fences > 0 {
				drew++
			}
		}
		lines = append(lines, fmt.Sprintf("%-8s %s (fences: %d)", mark, r.c.name, fences))
	}
	summary := fmt.Sprintf("model %s\ndrew %d of %d asked, %d unasked of %d, %d without a mermaid fence, %d errors\ntokens: %d prompt, %d completion\n%s\n",
		model, drew, drawWanted, unasked, len(results)-drawWanted, outsideFence, failed,
		spent.PromptTokens, spent.CompletionTokens, strings.Join(lines, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "summary.txt"), []byte(summary), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + summary)
}

type diagramEvalCase struct {
	name   string
	prompt string
	draw   bool
}

type diagramEvalResult struct {
	c      diagramEvalCase
	answer string
	err    error
}

// diagramWithoutFence says an answer holds a diagram's first line outside a
// mermaid fence.
func diagramWithoutFence(answer string) bool {
	for _, line := range strings.Split(answer, "\n") {
		l := strings.TrimSpace(line)
		for _, head := range []string{"flowchart ", "graph TD", "graph LR", "sequenceDiagram", "stateDiagram", "erDiagram", "mindmap", "timeline", "quadrantChart"} {
			if strings.HasPrefix(l, head) {
				return true
			}
		}
	}
	return false
}

var diagramEvalCases = []diagramEvalCase{
	// --- asked for, or shaped like one ---
	{name: "draw-mindmap-fair-en", prompt: "Make a mind map of the topics for planning a neighbourhood summer fair.", draw: true},
	{name: "draw-mindmap-move-de", prompt: "Erstelle eine Mindmap zum Thema Umzug in eine neue Stadt.", draw: true},
	{name: "draw-flow-overdue-en", prompt: "Draw a flowchart of how a library handles an overdue book.", draw: true},
	{name: "draw-flow-complaint-de", prompt: "Wie läuft eine Reklamation im Onlineshop ab, je nachdem ob die Ware beschädigt ist oder nicht? Zeichne es als Ablauf.", draw: true},
	{name: "draw-sequence-oauth-en", prompt: "How does the authorization code flow work between the browser, the app and the identity provider? Show the message exchange.", draw: true},
	{name: "draw-state-parcel-en", prompt: "What states does a parcel go through from order to delivery, including failed delivery attempts?", draw: true},
	{name: "draw-er-gym-en", prompt: "Design a database model for a small gym: members, memberships, classes and bookings.", draw: true},
	{name: "draw-tree-school-en", prompt: "Show me the structure of a typical secondary school, from the board down to the teachers.", draw: true},
	{name: "draw-state-order-de", prompt: "Welche Zustände hat eine Bestellung in einem Onlineshop? Zeig mir das als Diagramm.", draw: true},
	{name: "draw-flow-review-en", prompt: "Explain how a pull request gets reviewed and merged, including what happens when the checks fail.", draw: true},
	{name: "draw-timeline-garden-en", prompt: "Lay out the year for a new allotment garden as a timeline, season by season.", draw: true},
	{name: "draw-quadrant-chores-de", prompt: "Ordne diese Haushaltsaufgaben nach Aufwand und Nutzen ein: Fenster putzen, Keller entrümpeln, Steuererklärung, Pflanzen umtopfen, Fahrrad ölen.", draw: true},
	// --- none of these is a diagram ---
	{name: "none-buttermilk-en", prompt: "What's a good substitute for buttermilk in pancakes?"},
	{name: "none-egg-de", prompt: "Wie lange muss ein Ei kochen, damit es wachsweich ist?"},
	{name: "none-translate-en", prompt: "Translate 'see you tomorrow' into French and Italian."},
	{name: "none-birthday-en", prompt: "Write a short birthday message for my colleague."},
	{name: "none-chart-running-en", prompt: "Plot my weekly running distance: 12, 15, 9 and 20 km."},
	{name: "none-capital-en", prompt: "What's the capital of Australia?"},
	{name: "none-image-fox-en", prompt: "Draw a red fox in the snow."},
	{name: "none-steps-pasta-en", prompt: "What are the steps to cook pasta?"},
	{name: "none-interest-de", prompt: "Erklär mir den Unterschied zwischen Zinseszins und einfachem Zins."},
	{name: "none-cars-en", prompt: "Summarize the pros and cons of electric cars in a few sentences."},
	{name: "none-year-en", prompt: "In which year did the first person walk on the moon?"},
	{name: "none-packing-de", prompt: "Was gehört auf die Packliste für eine Wanderung übers Wochenende?"},
}
