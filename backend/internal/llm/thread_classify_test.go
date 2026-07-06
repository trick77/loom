package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/classifier"
)

func TestMessageContainsURL(t *testing.T) {
	cases := map[string]bool{
		"summarize https://example.com/post please": true,
		"what is on HTTP://EXAMPLE.COM":             true, // scheme, case-insensitive
		"check www.example.com for me":              true,
		"check (www.example.com) for me":            true, // leading punctuation trimmed
		"see \"www.example.ch\"":                    true,
		"Fass «www.blick.ch» zusammen":              true,  // Unicode quotes are a boundary
		"schau bei [Blick](www.blick.ch)":           true,  // markdown link, scheme-less
		"what does the prefix www. stand for":       false, // bare "www." with no domain after
		"how do I use node.js streams":              false, // bare domain-lookalike must not match
		"example.com without a scheme":              false, // deliberate: no bare-domain matching
		"Agents are downstream of context. Which thread here says this and what does it mean": false,
		"was heisst awww. auf englisch": false, // "www." must prefix the token
		"":                              false,
	}
	for in, want := range cases {
		if got := messageContainsURL(in); got != want {
			t.Errorf("messageContainsURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestThreadClassifySystemPromptWithholdsURLLookup(t *testing.T) {
	// The instructions sentence names url_lookup, so probe for the menu line.
	urlLookupLine := "- " + string(classifier.URLLookup) + ":"
	// No URL in the message: url_lookup is withheld from the offered menu.
	without := threadClassifySystemPrompt("which thread here says this and what does it mean")
	if strings.Contains(without, urlLookupLine) {
		t.Errorf("prompt without a URL must not offer %q", classifier.URLLookup)
	}
	// A pasted URL restores it.
	with := threadClassifySystemPrompt("summarize https://example.com/post")
	if !strings.Contains(with, urlLookupLine) {
		t.Errorf("prompt with a URL must offer %q", classifier.URLLookup)
	}
	// Both variants still offer the rest of the menu.
	for _, p := range []string{without, with} {
		if !strings.Contains(p, "- "+string(classifier.KnowledgeDiscovery)+":") {
			t.Errorf("prompt missing %q", classifier.KnowledgeDiscovery)
		}
	}
}

// TestClassifyThread_eval is the model-backed proof that the classify prompt
// routes by MEANING: deictic prompts without a URL must not land in url_lookup,
// real URLs still must, and the newer categories fire across languages. It hits
// a real endpoint, so it is skipped unless LOOM_THREAD_CLASSIFY_EVAL_BASEURL is
// set (CI has no model). Run it against a live backend with:
//
//	LOOM_THREAD_CLASSIFY_EVAL_BASEURL=http://localhost:1234/v1 \
//	LOOM_THREAD_CLASSIFY_EVAL_APIKEY=... \
//	go test ./internal/llm/ -run TestClassifyThread_eval -v
//
// Cases are common-case, not exhaustive; a single failure flags a prompt
// regression to investigate, not a hard contract on every phrasing.
func TestClassifyThread_eval(t *testing.T) {
	baseURL := os.Getenv("LOOM_THREAD_CLASSIFY_EVAL_BASEURL")
	if baseURL == "" {
		t.Skip("set LOOM_THREAD_CLASSIFY_EVAL_BASEURL to run the live classify eval")
	}
	c := NewClient(Config{BaseURL: baseURL, APIKey: os.Getenv("LOOM_THREAD_CLASSIFY_EVAL_APIKEY"), Timeout: 30 * time.Second}, nil)

	cases := []struct {
		msg  string
		want []classifier.Category // any of these passes
	}{
		// The original misclassification: deictic pointers, no URL. url_lookup is
		// withheld from the menu and coerced away, so any answer but url_lookup is
		// structurally guaranteed — the eval asserts the model picks a sensible one.
		{msg: "Agents are downstream of context. Which thread here says this and what does it mean", want: []classifier.Category{classifier.KnowledgeDiscovery, classifier.General}},
		// A pasted URL still routes to url_lookup.
		{msg: "https://example.com/blog/agents — what does this article claim?", want: []classifier.Category{classifier.URLLookup}},
		// The newer categories, across languages.
		{msg: "Was hilft gegen Kopfschmerzen ohne Medikamente?", want: []classifier.Category{classifier.Health}},
		{msg: "recommend me a series like Severance", want: []classifier.Category{classifier.EntertainmentRecs}},
		{msg: "où bien manger une fondue à Fribourg?", want: []classifier.Category{classifier.LocalInfo}},
		{msg: "Lohnt sich ein ETF-Sparplan bei den aktuellen Zinsen?", want: []classifier.Category{classifier.FinanceInvesting}},
	}

	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			got, err := c.ClassifyThread(context.Background(), tc.msg)
			if err != nil {
				t.Fatalf("ClassifyThread: %v", err)
			}
			for _, w := range tc.want {
				if got == string(w) {
					return
				}
			}
			t.Errorf("ClassifyThread(%q) = %q, want one of %v", tc.msg, got, tc.want)
		})
	}
}
