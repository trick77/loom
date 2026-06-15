//go:build liveembed

package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/trick77/lume/internal/store"
)

// TestLiveEmbedAndRetrieve exercises the real embeddings endpoint + real
// sqlite-vec KNN: it confirms the configured model returns 1536-dim vectors and
// that a semantically related query retrieves the right chunk. Run with:
//
//	BACKEND_EMBED_BASE_URL=... BACKEND_EMBED_API_KEY=... BACKEND_EMBED_MODEL=... \
//	  go test -tags liveembed -run TestLiveEmbedAndRetrieve ./internal/rag/ -v
func TestLiveEmbedAndRetrieve(t *testing.T) {
	emb := NewEmbedClient(EmbedConfig{
		BaseURL: os.Getenv("BACKEND_EMBED_BASE_URL"),
		APIKey:  os.Getenv("BACKEND_EMBED_API_KEY"),
		Model:   os.Getenv("BACKEND_EMBED_MODEL"),
	}, nil)
	ctx := context.Background()

	docs := []string{
		"The lume backend is built in Go and serves an embedded React SPA.",
		"Bananas are a good source of potassium and grow in tropical climates.",
		"Apache Tika extracts plain text from PDF, DOCX, and other documents.",
	}
	embeddedDocs, err := emb.Embed(ctx, docs)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	vecs := embeddedDocs.Vectors
	if len(vecs) != 3 {
		t.Fatalf("got %d vectors, want 3", len(vecs))
	}
	if len(vecs[0]) != 1536 {
		t.Fatalf("embedding dim = %d, want 1536 (vec_chunks is float[1536])", len(vecs[0]))
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	_ = s.CreateDocument(ctx, Document{ID: "d", UserID: "u", VolumeRelpath: "files/d", Filename: "d.txt", MIME: "text/plain", Status: StatusPending})
	chunks := make([]TextChunk, len(docs))
	for i, d := range docs {
		chunks[i] = TextChunk{Ordinal: i, Text: d, TokenCount: 1}
	}
	if err := s.ReplaceChunks(ctx, "u", "d", chunks, vecs); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	embeddedQuery, err := emb.Embed(ctx, []string{"What language is the lume server written in?"})
	if err != nil {
		t.Fatalf("Embed query: %v", err)
	}
	res, err := s.Retrieve(ctx, "u", nil, embeddedQuery.Vectors[0], 1)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1", len(res))
	}
	if res[0].Ordinal != 0 {
		t.Errorf("nearest chunk ordinal = %d (%q), want 0 (the Go/lume sentence)", res[0].Ordinal, res[0].Text)
	}
	t.Logf("real KNN top hit: %q", res[0].Text)
}
