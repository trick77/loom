package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/web"
)

// TestInjectShareMeta_injectsAndEscapes verifies the OG/Twitter tags are inserted
// once before </head>, the tab title is rewritten, the SPA boot markup is preserved,
// and a hostile title/author cannot break out of the meta attribute.
func TestInjectShareMeta_injectsAndEscapes(t *testing.T) {
	index := `<!doctype html><html><head><title>Loom</title></head>` +
		`<body><div id="root"></div><script src="/app.js"></script></body></html>`

	out := injectShareMeta(index, `Tune "PG" <fast>`, `A & B`,
		"https://loom.example.com/share/abc", "https://loom.example.com/og-card.png")

	// Card tags present with the escaped title (no raw quote/angle bracket leaks).
	for _, want := range []string{
		`<meta property="og:title" content="Tune &#34;PG&#34; &lt;fast&gt;" />`,
		`<meta property="og:description" content="Shared by A &amp; B · Loom" />`,
		`<meta property="og:url" content="https://loom.example.com/share/abc" />`,
		`<meta property="og:image" content="https://loom.example.com/og-card.png" />`,
		`<meta name="twitter:card" content="summary_large_image" />`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	// No unescaped title escapes the attribute.
	if strings.Contains(out, `content="Tune "PG"`) {
		t.Error("title was not HTML-escaped inside the attribute")
	}
	// Tab title rewritten; exactly one </head>; SPA boot markup intact.
	if !strings.Contains(out, `<title>Tune &#34;PG&#34; &lt;fast&gt; · Loom</title>`) {
		t.Error("tab <title> was not rewritten")
	}
	if n := strings.Count(out, "</head>"); n != 1 {
		t.Errorf("</head> count = %d, want 1", n)
	}
	if !strings.Contains(out, `<script src="/app.js"></script>`) || !strings.Contains(out, `id="root"`) {
		t.Error("SPA boot markup was altered")
	}
}

// TestInjectShareMeta_defaultsWhenEmpty falls back to a generic title/description
// when the share has no title/author (no "Shared by" and no empty <title>).
func TestInjectShareMeta_defaultsWhenEmpty(t *testing.T) {
	out := injectShareMeta(`<head><title>Loom</title></head>`, "  ", "",
		"https://x/share/a", "https://x/og-card.png")
	if !strings.Contains(out, `content="Shared conversation"`) {
		t.Errorf("missing default title: %s", out)
	}
	if strings.Contains(out, "Shared by") {
		t.Errorf("should not emit author line when empty: %s", out)
	}
	if !strings.Contains(out, `content="Shared on Loom"`) {
		t.Errorf("missing default description: %s", out)
	}
}

// TestShareIndex_servesCardForActiveShare drives the full server: an anonymous
// GET /share/{id} for an active share returns the enriched HTML with the thread
// title in the card, an absolute og:image, and the noindex header intact.
func TestShareIndex_servesCardForActiveShare(t *testing.T) {
	store := &fakeThreadStore{shares: map[string]chat.Share{
		"t1": {
			ShareID:  "abc123",
			ThreadID: "t1",
			Shared:   true,
			Title:    "My Thread",
			Snapshot: json.RawMessage(`{"author":"Jan"}`),
		},
	}}
	srv := New(Deps{Thread: store, Static: web.SPAHandler(), PublicURL: "https://loom.example.com"})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/abc123", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<meta property="og:title" content="My Thread" />`) {
		t.Errorf("missing og:title for the thread\n%s", body)
	}
	if !strings.Contains(body, `content="Shared by Jan · Loom"`) {
		t.Error("missing author-derived description")
	}
	if !strings.Contains(body, `content="https://loom.example.com/og-card.png"`) {
		t.Error("missing absolute og:image")
	}
	if !strings.Contains(body, `content="https://loom.example.com/share/abc123"`) {
		t.Error("missing absolute og:url")
	}
	if got := rec.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
}

// TestShareIndex_deadLinkServesPlainIndex verifies an unknown/disabled share serves
// the plain SPA index with no card (no title leak, no existence oracle in the meta).
func TestShareIndex_deadLinkServesPlainIndex(t *testing.T) {
	srv := New(Deps{Thread: &fakeThreadStore{}, Static: web.SPAHandler(), PublicURL: "https://loom.example.com"})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/does-not-exist", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "og:title") {
		t.Error("dead link must not emit an og:title card")
	}
	if got := rec.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
}
