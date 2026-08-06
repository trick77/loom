package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// titleServer replies to every chat completion with the given title.
func titleServer(t *testing.T, content string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": content}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return NewClient(Config{BaseURL: server.URL}, server.Client())
}

func TestGenerateThreadTitleRejectsScriptDrift(t *testing.T) {
	cases := []struct {
		name             string
		modelTitle       string
		userMessage      string
		assistantMessage string
		want             string
	}{
		{
			// The reported regression: an English question, an English answer,
			// and a Chinese title invented by the model.
			name:             "chinese title over an english turn is discarded",
			modelTitle:       "Kasia Knez婚姻查询",
			userMessage:      "kasia newyadoma is married to whom?",
			assistantMessage: "You're likely thinking of Katarzyna Niewiadoma, who married Taylor Phinney in May 2024.",
			want:             "",
		},
		{
			name:             "english title over an english turn is kept",
			modelTitle:       "Niewiadoma Marriage",
			userMessage:      "kasia newyadoma is married to whom?",
			assistantMessage: "She married Taylor Phinney in May 2024.",
			want:             "Niewiadoma Marriage",
		},
		{
			// A user writing Chinese must still get a Chinese title.
			name:             "chinese title over a chinese question is kept",
			modelTitle:       "婚姻查询",
			userMessage:      "她和谁结婚了？",
			assistantMessage: "She married Taylor Phinney in May 2024.",
			want:             "婚姻查询",
		},
		{
			// The answer is a source too: a term it introduced is legitimate.
			name:             "script introduced by the answer is kept",
			modelTitle:       "Meaning of 幽玄",
			userMessage:      "what does yugen mean",
			assistantMessage: "幽玄 (yūgen) is a concept in Japanese aesthetics.",
			want:             "Meaning of 幽玄",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := titleServer(t, tc.modelTitle)
			got, err := client.GenerateThreadTitle(context.Background(), tc.userMessage, tc.assistantMessage, "English")
			if err != nil {
				t.Fatalf("GenerateThreadTitle() error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("GenerateThreadTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGenerateReasoningTitleRejectsScriptDrift(t *testing.T) {
	// Unpinned language: the reasoning trace is the only signal, and a title in
	// a script it never used is drift.
	client := titleServer(t, "解释天空颜色")
	got, err := client.GenerateReasoningTitle(context.Background(), "The user wants to know why the sky is blue.", "")
	if err != nil {
		t.Fatalf("GenerateReasoningTitle() error: %v", err)
	}
	if got != "" {
		t.Fatalf("GenerateReasoningTitle() = %q, want %q", got, "")
	}
}

func TestGenerateReasoningTitleSkipsGuardWhenLanguageIsPinned(t *testing.T) {
	// A pinned language may legitimately differ in script from the model's own
	// reasoning trace, so comparing the two would reject correct titles. Guarding
	// here would drop every reasoning title for a non-Latin pinned language.
	client := titleServer(t, "解释天空颜色")
	got, err := client.GenerateReasoningTitle(context.Background(), "The user wants to know why the sky is blue.", "Chinese")
	if err != nil {
		t.Fatalf("GenerateReasoningTitle() error: %v", err)
	}
	if got != "解释天空颜色" {
		t.Fatalf("GenerateReasoningTitle() = %q, want the model's title kept", got)
	}
}

func TestAppendLanguageDirective(t *testing.T) {
	cases := []struct {
		name             string
		responseLanguage string
		want             string
	}{
		{
			name:             "pinned language is named",
			responseLanguage: "German",
			want:             "Always write your reply in this language: German.",
		},
		{
			// Unset used to append nothing, leaving a Chinese-trained model to
			// choose for itself.
			name:             "unset follows the source material",
			responseLanguage: "",
			want:             "Always write your reply in the same language as the material you are given. If that is unclear, write it in English.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := appendLanguageDirective("BASE.", tc.responseLanguage)
			if !strings.HasPrefix(got, "BASE.") {
				t.Fatalf("appendLanguageDirective() = %q, want the base prompt preserved", got)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("appendLanguageDirective() = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
