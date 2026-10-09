package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// Every argument-taking built-in tool used to parse its arguments in its own
// copy of the same block; one path now serves them all.
func TestExecuteBuiltInToolReportsInvalidArgumentsForEveryArgTool(t *testing.T) {
	s := &Engine{}
	for _, spec := range coreTools {
		name := spec.name
		if name == ProjectThreadsToolName {
			continue // takes no arguments
		}
		call := llm.ToolCall{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: name, Arguments: "{not json"}}
		run := &Run{e: s, user: testUser, thread: chat.Thread{ID: "t1"}}
		output, resp, handled := run.executeBuiltInTool(context.Background(), call)
		if !handled || resp != nil {
			t.Fatalf("%s: handled=%v resp=%v, want handled with no artifact", name, handled, resp)
		}
		if !strings.HasPrefix(output, "tool failed: invalid arguments") {
			t.Fatalf("%s: output = %q, want the invalid-arguments failure", name, output)
		}
	}
}

// executeToolCall runs one MCP call start to finish for user, outside a turn.
func (s *Engine) executeToolCall(ctx context.Context, user auth.User, call llm.ToolCall, round int, reg *webSourceRegistry) string {
	return (&Run{e: s, user: user}).finishToolCall(ctx, call, round, reg, s.runToolCall(ctx, call))
}

func TestAvailableToolsSkipsMCPDuplicateOfBuiltInTool(t *testing.T) {
	srv := &Engine{
		artifacts: fakeArtifactStore{},
		usersDir:  t.TempDir(),
		docTools:  []docgen.Generator{docgen.TextGenerator{}},
		mcp: fakeMCPService{ToolList: []llm.Tool{
			{Type: "function", Function: llm.ToolFunction{Name: "create_text_file"}},
			{Type: "function", Function: llm.ToolFunction{Name: "search__web"}},
		}},
	}

	// Gate with the coding category so the docgen tools are offered (this test is
	// about de-duping an MCP tool against a built-in, not about gating).
	tools := srv.availableTools(chat.Thread{}, newToolGate(string(classifier.Coding), "", ""))

	var builtInCount, searchCount int
	for _, tool := range tools {
		switch tool.Function.Name {
		case "create_text_file":
			builtInCount++
		case "search__web":
			searchCount++
		}
	}
	if builtInCount != 1 || searchCount != 1 {
		t.Fatalf("tool counts create_text_file=%d search__web=%d, want 1 and 1", builtInCount, searchCount)
	}
}

func TestExecuteToolCallFetchObscuraFallback(t *testing.T) {
	fetchCall := llm.ToolCall{
		Function: llm.ToolCallFunction{
			Name:      fetchToolName,
			Arguments: `{"url":"https://example.com"}`,
		},
	}

	t.Run("falls back to obscura when fetch fails", func(t *testing.T) {
		var navigated bool
		srv := &Engine{mcp: fakeMCPService{
			Available: map[string]bool{
				obscuraNavigateToolName: true,
				obscuraSnapshotToolName: true,
			},
			CallFunc: func(_ context.Context, name string, args map[string]any) (string, error) {
				switch name {
				case fetchToolName:
					return "", errFakeTool
				case obscuraNavigateToolName:
					if args["url"] != "https://example.com" {
						t.Fatalf("navigate url = %v, want https://example.com", args["url"])
					}
					navigated = true
					return "ok", nil
				case obscuraSnapshotToolName:
					if !navigated {
						t.Fatal("snapshot called before navigate")
					}
					return "rendered page text", nil
				}
				return "", errFakeTool
			},
		}}

		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, fetchCall, 0, newWebSourceRegistryAfter(0))

		if !strings.Contains(got, "rendered page text") {
			t.Fatalf("output = %q, want obscura snapshot text", got)
		}
		// The fallback fetched https://example.com, so its snapshot is annotated
		// with that source's [n] marker for inline citation.
		if !strings.HasPrefix(got, "Web source [1]: https://example.com") {
			t.Fatalf("output = %q, want leading web-source marker", got)
		}
	})

	t.Run("skips obscura for a failed PDF extraction", func(t *testing.T) {
		srv := &Engine{mcp: fakeMCPService{
			Available: map[string]bool{
				obscuraNavigateToolName: true,
				obscuraSnapshotToolName: true,
			},
			CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				if name == fetchToolName {
					return "", mcp.PDFExtractionError{Err: errors.New("Failed to extract PDF x: no extractable text")}
				}
				t.Fatalf("obscura tool %q called for a PDF extraction failure", name)
				return "", nil
			},
		}}

		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, fetchCall, 0, newWebSourceRegistryAfter(0))

		if !strings.Contains(got, "no extractable text") {
			t.Fatalf("output = %q, want the extraction error", got)
		}
	})

	// A fetch that ran into its deadline leaves an expired context behind; the
	// fallback must get its own budget or it can never rescue a timed-out fetch.
	t.Run("fallback gets its own deadline", func(t *testing.T) {
		var fetchDeadline, navigateDeadline time.Time
		srv := &Engine{mcp: fakeMCPService{
			Available: map[string]bool{
				obscuraNavigateToolName: true,
				obscuraSnapshotToolName: true,
			},
			CallFunc: func(ctx context.Context, name string, _ map[string]any) (string, error) {
				switch name {
				case fetchToolName:
					fetchDeadline, _ = ctx.Deadline()
					time.Sleep(2 * time.Millisecond)
					return "", errFakeTool
				case obscuraNavigateToolName:
					navigateDeadline, _ = ctx.Deadline()
				}
				return "ok", nil
			},
		}}

		srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, fetchCall, 0, newWebSourceRegistryAfter(0))

		if !navigateDeadline.After(fetchDeadline) {
			t.Fatalf("navigate deadline %v not after fetch deadline %v", navigateDeadline, fetchDeadline)
		}
	})

	t.Run("surfaces fetch failure when obscura is unavailable", func(t *testing.T) {
		srv := &Engine{mcp: fakeMCPService{Err: errFakeTool}}

		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, fetchCall, 0, newWebSourceRegistryAfter(0))

		if !strings.HasPrefix(got, "tool failed") {
			t.Fatalf("output = %q, want tool failed prefix", got)
		}
	})

	t.Run("does not fall back for non-fetch tools", func(t *testing.T) {
		var obscuraCalled bool
		srv := &Engine{mcp: fakeMCPService{
			Available: map[string]bool{
				obscuraNavigateToolName: true,
				obscuraSnapshotToolName: true,
			},
			CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				if name == obscuraNavigateToolName || name == obscuraSnapshotToolName {
					obscuraCalled = true
				}
				return "", errFakeTool
			},
		}}
		otherCall := llm.ToolCall{Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{"query":"x"}`}}

		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, otherCall, 0, newWebSourceRegistryAfter(0))

		if obscuraCalled {
			t.Fatal("obscura must not be called for non-fetch tools")
		}
		if !strings.HasPrefix(got, "tool failed") {
			t.Fatalf("output = %q, want tool failed prefix", got)
		}
	})
}

func TestStartToolRuns(t *testing.T) {
	fetch := func(url string) llm.ToolCall {
		return llm.ToolCall{Function: llm.ToolCallFunction{Name: fetchToolName, Arguments: fmt.Sprintf(`{"url":%q}`, url)}}
	}
	search := llm.ToolCall{Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}

	t.Run("a lone eligible call stays on the sequential path", func(t *testing.T) {
		srv := &Engine{mcp: fakeMCPService{Result: "ok"}}
		runs := srv.startToolRuns(context.Background(), []llm.ToolCall{fetch("https://a.example"), search}, []bool{false, false})
		if runs[0] != nil || runs[1] != nil {
			t.Fatalf("runs = %v, want none started", runs)
		}
	})

	t.Run("skipped and stateful calls are not started", func(t *testing.T) {
		srv := &Engine{mcp: fakeMCPService{Result: "ok"}}
		calls := []llm.ToolCall{fetch("https://a.example"), search, fetch("https://b.example"), fetch("https://c.example")}
		runs := srv.startToolRuns(context.Background(), calls, []bool{false, false, false, true})
		if runs[0] == nil || runs[2] == nil || runs[1] != nil || runs[3] != nil {
			t.Fatalf("runs = %v, want only the two live fetches started", runs)
		}
		if got := <-runs[0]; got.err != nil || got.output != "ok" {
			t.Fatalf("run = %+v, want the tool output", got)
		}
		<-runs[2]
	})

	t.Run("a panicking call fails that call only", func(t *testing.T) {
		srv := &Engine{mcp: fakeMCPService{CallFunc: func(_ context.Context, _ string, args map[string]any) (string, error) {
			if args["url"] == "https://a.example" {
				panic("boom")
			}
			return "ok", nil
		}}}
		runs := srv.startToolRuns(context.Background(), []llm.ToolCall{fetch("https://a.example"), fetch("https://b.example")}, []bool{false, false})
		if got := <-runs[0]; got.err == nil {
			t.Fatal("panicking run reported no error")
		}
		if got := <-runs[1]; got.err != nil || got.output != "ok" {
			t.Fatalf("run = %+v, want the tool output", got)
		}
	})

	t.Run("a cancelled round releases the calls still waiting for a slot", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		release := make(chan struct{})
		srv := &Engine{mcp: fakeMCPService{CallFunc: func(context.Context, string, map[string]any) (string, error) {
			<-release
			return "ok", nil
		}}}
		calls := make([]llm.ToolCall, concurrentToolRuns+1)
		for i := range calls {
			calls[i] = fetch(fmt.Sprintf("https://%d.example", i))
		}
		runs := srv.startToolRuns(ctx, calls, make([]bool, len(calls)))
		merged := make(chan toolRun, len(runs))
		for _, run := range runs {
			go func() { merged <- <-run }()
		}
		cancel()
		// Every slot holder is still blocked in its call, so the first run to
		// finish can only be the one that was waiting for a slot.
		if got := <-merged; !errors.Is(got.err, context.Canceled) {
			t.Fatalf("first finished run = %+v, want the cancelled waiter", got)
		}
		close(release)
		for range len(runs) - 1 {
			<-merged
		}
	})
}
