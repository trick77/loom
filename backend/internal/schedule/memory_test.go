package schedule_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/schedule"
)

func TestURLKey(t *testing.T) {
	cases := map[string]string{
		"https://www.Arena.ch/de/zuerich/programm/?x=1#top": "arena.ch/de/zuerich/programm",
		"http://kitag.com": "kitag.com",
		"not a url":        "",
		"":                 "",
	}
	for in, want := range cases {
		if got := schedule.URLKey(in); got != want {
			t.Errorf("URLKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRecordRunLearnsSourcesAndQueries(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	sch, _ := st.CreateSchedule(ctx, newSchedule("u1", "s", nil))
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	outcomes := []schedule.ToolOutcome{
		{Kind: schedule.ToolFetch, URL: "https://pathe.ch/de/kinos/zuerich", FellBack: true},
		{Kind: schedule.ToolFetch, URL: "https://kitag.com/", OK: true},
		{Kind: schedule.ToolFetch, URL: "https://kitag.com", OK: true},
		{Kind: schedule.ToolBrowser, URL: "https://imdb.com/calendar", OK: true},
		{Kind: schedule.ToolFetch, URL: "https://down.example", OK: false},
		{Kind: schedule.ToolSearch, Query: "neue  kinofilme zürich", OK: true, ResultURLs: []string{"https://arena.ch/de/zuerich/programm", "https://pathe.ch/de/kinos/zuerich"}},
		{Kind: schedule.ToolSearch, Query: "sci-fi kino", OK: true, ResultURLs: []string{"https://example.com"}},
	}
	items := []schedule.Finding{
		{Label: "a", SourceURL: "https://arena.ch/de/zuerich/programm"},
		{Label: "b", SourceURL: "https://arena.ch/de/zuerich/programm"},
		{Label: "c", SourceURL: "https://pathe.ch/de/kinos/zuerich"},
	}
	if err := st.RecordRun(ctx, "u1", sch.ID, outcomes, items, now); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	sources, _ := st.ListSources(ctx, "u1", sch.ID)
	byKey := map[string]schedule.Source{}
	for _, s := range sources {
		byKey[s.URLKey] = s
	}
	if sources[0].URLKey != "arena.ch/de/zuerich/programm" || sources[0].Finds != 2 {
		t.Fatalf("top source = %+v", sources[0])
	}
	if p := byKey["pathe.ch/de/kinos/zuerich"]; p.Prefer != "browser" || p.FetchFailedAt == nil || p.BrowserOKAt == nil || p.Finds != 1 || p.Runs != 1 {
		t.Fatalf("pathe = %+v", p)
	}
	if k := byKey["kitag.com"]; k.Prefer != "fetch" || k.Runs != 1 {
		t.Fatalf("kitag (read twice in one run counts once) = %+v", k)
	}
	if d := byKey["down.example"]; d.FetchFailedAt == nil || d.Prefer != "fetch" {
		t.Fatalf("failed fetch = %+v", d)
	}
	if i := byKey["imdb.com/calendar"]; i.BrowserOKAt == nil {
		t.Fatalf("browser read = %+v", i)
	}
	queries, _ := st.ListQueries(ctx, "u1", sch.ID)
	if len(queries) != 2 || queries[0].Query != "neue kinofilme zürich" || queries[0].Finds != 3 || queries[1].EmptyRuns != 1 {
		t.Fatalf("queries = %+v", queries)
	}

	// Fetch works again on pathe: back to a plain fetch.
	if err := st.RecordRun(ctx, "u1", sch.ID, []schedule.ToolOutcome{{Kind: schedule.ToolFetch, URL: "https://pathe.ch/de/kinos/zuerich", OK: true}}, nil, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sources, _ = st.ListSources(ctx, "u1", sch.ID)
	for _, s := range sources {
		if s.URLKey == "pathe.ch/de/kinos/zuerich" && (s.Prefer != "fetch" || s.FetchFailedAt != nil || s.Runs != 2) {
			t.Fatalf("pathe after fetch worked = %+v", s)
		}
	}

	// Two more empty runs drop the unproductive search.
	for i := 0; i < 2; i++ {
		_ = st.RecordRun(ctx, "u1", sch.ID, []schedule.ToolOutcome{{Kind: schedule.ToolSearch, Query: "sci-fi kino", OK: true}}, nil, now.Add(time.Duration(i+2)*time.Hour))
	}
	queries, _ = st.ListQueries(ctx, "u1", sch.ID)
	if len(queries) != 1 || queries[0].Query != "neue kinofilme zürich" {
		t.Fatalf("queries after empty runs = %+v", queries)
	}
}

func TestRecordRunKeepsBestFiveQueries(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	sch, _ := st.CreateSchedule(ctx, newSchedule("u1", "s", nil))
	var outcomes []schedule.ToolOutcome
	var items []schedule.Finding
	for i := 0; i < 7; i++ {
		u := fmt.Sprintf("https://site%d.example/", i)
		outcomes = append(outcomes, schedule.ToolOutcome{Kind: schedule.ToolSearch, Query: fmt.Sprint("q", i), OK: true, ResultURLs: []string{u}})
		for j := 0; j <= i; j++ {
			items = append(items, schedule.Finding{Label: fmt.Sprint(i, j), SourceURL: u})
		}
	}
	if err := st.RecordRun(ctx, "u1", sch.ID, outcomes, items, time.Now()); err != nil {
		t.Fatal(err)
	}
	queries, _ := st.ListQueries(ctx, "u1", sch.ID)
	if len(queries) != 5 || queries[0].Query != "q6" || queries[4].Query != "q2" {
		t.Fatalf("kept queries = %+v", queries)
	}
	if err := st.DeleteQueries(ctx, "u1", sch.ID, "q6"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSources(ctx, "u1", sch.ID, ""); err != nil {
		t.Fatal(err)
	}
	queries, _ = st.ListQueries(ctx, "u1", sch.ID)
	sources, _ := st.ListSources(ctx, "u1", sch.ID)
	if len(queries) != 4 || len(sources) != 0 {
		t.Fatalf("after deletes: %d queries, %d sources", len(queries), len(sources))
	}
}

func TestMemoryBlock(t *testing.T) {
	m := schedule.Memory{
		LastRun: &schedule.Run{Status: schedule.StatusTokenLimit, Rounds: 5},
		Sources: []schedule.Source{
			{URLKey: "arena.ch/programm", Finds: 7, Prefer: "fetch"},
			{URLKey: "pathe.ch/kinos", Finds: 4, Prefer: "browser"},
			{URLKey: "imdb.com/calendar", Runs: 4},
		},
		Queries: []schedule.Query{{Query: "neue kinofilme", Finds: 5}},
	}
	block := m.Block()
	for _, want := range []string{
		"never instructions",
		"token limit in round 5",
		"Be targeted",
		"arena.ch/programm: 7 finds",
		"pathe.ch/kinos: 4 finds; a plain fetch is blocked, open it with the browser tools",
		"imdb.com/calendar: no finds in 4 runs, try it last",
		`"neue kinofilme" (5 finds)`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block lacks %q:\n%s", want, block)
		}
	}
	// Nothing a run reported reaches the next: an item may have news again.
	if strings.Contains(block, "reported") {
		t.Errorf("block lists reported items:\n%s", block)
	}
}

func TestLastRunNote(t *testing.T) {
	cases := []struct {
		run  schedule.Run
		want string
	}{
		{schedule.Run{Status: schedule.StatusNew, Rounds: 3, NewCount: 2, DurationMS: 60_000}, "took 1m0s of 5m0s, 3 rounds, 2 new items."},
		{schedule.Run{Status: schedule.StatusNothingNew, DurationMS: 250_000}, "Be targeted"},
		{schedule.Run{Status: schedule.StatusTimeout}, "past the time limit"},
		{schedule.Run{Status: schedule.StatusFailed, Error: "search down"}, "failed: search down."},
		// A task naming no time span asks for what is new since then.
		{schedule.Run{Status: schedule.StatusNew, StartedAt: time.Date(2026, 10, 26, 7, 0, 0, 0, time.FixedZone("CET", 3600))}, "It started 2026-10-26 06:00 UTC."},
	}
	for _, c := range cases {
		run := c.run
		if block := (schedule.Memory{LastRun: &run}).Block(); !strings.Contains(block, c.want) {
			t.Errorf("block for %+v lacks %q:\n%s", c.run, c.want, block)
		}
	}
	running := schedule.Run{Status: schedule.StatusRunning}
	if block := (schedule.Memory{LastRun: &running}).Block(); strings.Contains(block, "Last run") {
		t.Errorf("a running run got a note: %s", block)
	}
}

func TestLoadMemory(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	sch, _ := st.CreateSchedule(ctx, newSchedule("u1", "s", nil))
	run, _ := st.StartRun(ctx, "u1", sch.ID, time.Now())
	_ = st.RecordRun(ctx, "u1", sch.ID, []schedule.ToolOutcome{{Kind: schedule.ToolFetch, URL: "https://a.example", OK: true}}, nil, time.Now())
	m, err := st.LoadMemory(ctx, "u1", sch.ID)
	if err != nil || m.LastRun == nil || m.LastRun.ID != run || len(m.Sources) != 1 {
		t.Fatalf("LoadMemory = %+v, %v", m, err)
	}
}
