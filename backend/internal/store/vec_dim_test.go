package store

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/trick77/loom/internal/rag"
)

// The vec_chunks column width is fixed by the migration that created it, and
// the embedding model is a constant of the build whose dimension llmwire's
// profile states. The two must agree or every insert fails at the first upload.
func TestVecChunksWidthMatchesTheEmbeddingModel(t *testing.T) {
	ddl, err := migrationsFS.ReadFile("migrations/0005_documents_rag.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`embedding\s+float\[(\d+)\]`).FindSubmatch(ddl)
	if m == nil {
		t.Fatal("no float[N] embedding column in 0005_documents_rag.sql")
	}
	width, _ := strconv.Atoi(string(m[1]))
	if width != rag.EmbedDim() {
		t.Fatalf("vec_chunks embedding is float[%d], but %s returns %d dimensions", width, rag.EmbedModel, rag.EmbedDim())
	}
}
