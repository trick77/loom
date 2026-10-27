package llm

import (
	"context"
	"strings"
	"testing"
)

func TestParseSchedule(t *testing.T) {
	var body string
	srv := intentServer(t, "```json\n{\"template\":\"~  7~22 * * ~\",\"ok\":true}\n```", &body)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	template, ok, err := c.ParseSchedule(context.Background(), "einmal pro Woche")
	if err != nil || !ok || template != "~ 7~22 * * ~" {
		t.Fatalf("ParseSchedule = %q, %v, %v", template, ok, err)
	}
	if !strings.Contains(body, "einmal pro Woche") {
		t.Fatalf("the description did not reach the model: %s", body)
	}
}

func TestParseScheduleReplies(t *testing.T) {
	cases := map[string]bool{
		`{"ok":false}`:                        false,
		`{"template":"","ok":true}`:           false,
		`{"template":"0 9 * * *","ok":false}`: false,
		`not json`:                            false,
		`{"template":5}`:                      false,
		`{"template":"0 9 * * *","ok":true}`:  true,
	}
	for reply, want := range cases {
		if _, ok := parseScheduleReply(reply); ok != want {
			t.Errorf("parseScheduleReply(%q) ok = %v, want %v", reply, ok, want)
		}
	}
}

func TestExtractFindings(t *testing.T) {
	var body string
	srv := intentServer(t, `{"items":[{"key":"Dune: Part Three","label":"Dune:  Part Three","source":2},{"key":"dune-part-three","label":"dup"},{"key":"","label":"no key"},{"key":"avengers-doomsday","label":"","source":-1}]}`, &body)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	items, err := c.ExtractFindings(context.Background(), "find films", "Two new films [2]")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0] != (Finding{Key: "dune-part-three", Label: "Dune: Part Three", Source: 2}) {
		t.Fatalf("first = %+v", items[0])
	}
	if items[1] != (Finding{Key: "avengers-doomsday", Label: "avengers-doomsday", Source: 0}) {
		t.Fatalf("second = %+v", items[1])
	}
	// Earlier runs never reach the extractor: an item reported before may
	// have news again.
	if strings.Contains(body, "Known keys") || !strings.Contains(body, "Two new films [2]") {
		t.Fatalf("request names known keys or lacks the answer: %s", body)
	}
}

func TestParseFindingsBounds(t *testing.T) {
	long := strings.Repeat("a", 100)
	items, ok := parseFindings(`{"items":[{"key":"` + long + `","label":"` + long + `"}]}`)
	if !ok || len(items) != 1 || len(items[0].Key) != 80 || len([]rune(items[0].Label)) != 80 {
		t.Fatalf("items = %+v (ok %v)", items, ok)
	}
	for _, bad := range []string{"nothing", `{"items":"x"}`, `{}`, `{"items":[{"key":"デューン","label":"Dune"},{"key":""}]}`} {
		if items, ok := parseFindings(bad); ok || items != nil {
			t.Fatalf("parseFindings(%q) = %+v, %v; want no items, not ok", bad, items, ok)
		}
	}
	if items, ok := parseFindings(`{"items":[]}`); !ok || len(items) != 0 {
		t.Fatalf("an empty list = %+v, %v; want ok with no items", items, ok)
	}
}

// A reply the extractor cannot read is an error, never "nothing new": the
// runner drops a nothing-new run's thread, and must not drop a real answer.
func TestExtractFindingsUnreadableReplyIsAnError(t *testing.T) {
	for _, reply := range []string{"I found nothing new.", `{"items":"x"}`} {
		srv := intentServer(t, reply, new(string))
		c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
		if items, err := c.ExtractFindings(context.Background(), "find films", "answer"); err == nil {
			t.Fatalf("reply %q: items %+v, no error", reply, items)
		}
		srv.Close()
	}
	srv := intentServer(t, `{"items":[]}`, new(string))
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	if items, err := c.ExtractFindings(context.Background(), "find films", "nothing new"); err != nil || len(items) != 0 {
		t.Fatalf("an empty list = %+v, %v; want no items and no error", items, err)
	}
}
