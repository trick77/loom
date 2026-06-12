package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedClient_Embed_postsInputsAndReturnsVectors(t *testing.T) {
	var gotModel string
	var gotInput []string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		gotModel = req.Model
		gotInput = req.Input

		resp := map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}},
				{"index": 1, "embedding": []float64{0.4, 0.5, 0.6}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewEmbedClient(EmbedConfig{BaseURL: srv.URL, APIKey: "sk-test", Model: "text-embedding-3-small"}, nil)
	vecs, err := c.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}

	if gotModel != "text-embedding-3-small" {
		t.Errorf("model = %q, want text-embedding-3-small", gotModel)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("auth = %q, want Bearer sk-test", gotAuth)
	}
	if len(gotInput) != 2 || gotInput[0] != "hello" || gotInput[1] != "world" {
		t.Errorf("input = %v, want [hello world]", gotInput)
	}
	if len(vecs) != 2 {
		t.Fatalf("returned %d vectors, want 2", len(vecs))
	}
	if len(vecs[0]) != 3 || vecs[1][0] != 0.4 {
		t.Errorf("vectors = %v, want aligned 3-dim embeddings", vecs)
	}
}

func TestEmbedClient_Embed_emptyInputReturnsNothing(t *testing.T) {
	c := NewEmbedClient(EmbedConfig{BaseURL: "http://unused", Model: "m"}, nil)
	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil) error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("Embed(nil) = %d vectors, want 0", len(vecs))
	}
}

func TestEmbedClient_Embed_errorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewEmbedClient(EmbedConfig{BaseURL: srv.URL, Model: "m"}, nil)
	if _, err := c.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("Embed() error = nil, want error on 429")
	}
}
