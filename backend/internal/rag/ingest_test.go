package rag

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeExtractor struct {
	text string
	err  error
}

func (f fakeExtractor) Extract(_ context.Context, _, _ string, r io.Reader) (string, error) {
	io.Copy(io.Discard, r)
	return f.text, f.err
}

type fakeEmbedder struct {
	gotInputs [][]string
	err       error
}

func (f *fakeEmbedder) Embed(_ context.Context, inputs []string) (EmbedResult, error) {
	if f.err != nil {
		return EmbedResult{}, f.err
	}
	f.gotInputs = append(f.gotInputs, append([]string(nil), inputs...))
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = unit()
	}
	return EmbedResult{Vectors: out, Usage: EmbeddingUsage{PromptTokens: len(inputs), TotalTokens: len(inputs), Present: true}}, nil
}

type fakeUsageRecorder struct {
	userID   string
	tokens   int
	requests int
	calls    int
}

func (f *fakeUsageRecorder) AddEmbeddingUsage(_ context.Context, userID string, tokens, requests int) error {
	f.userID = userID
	f.tokens += tokens
	f.requests += requests
	f.calls++
	return nil
}

type fakeOpener struct {
	err error
}

func (f fakeOpener) OpenDocument(_ Document) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader("raw bytes")), nil
}

func newIngester(t *testing.T, ext Extractor, emb Embedder, op FileOpener) (*Ingester, *Store) {
	t.Helper()
	s, _ := newTestStore(t)
	return NewIngester(s, op, ext, emb), s
}

func TestIngester_Ingest_happyPath(t *testing.T) {
	emb := &fakeEmbedder{}
	usage := &fakeUsageRecorder{}
	ing, s := newIngester(t,
		fakeExtractor{text: strings.Repeat("word ", 100000)},
		emb,
		fakeOpener{})
	ing.SetUsageRecorder(usage)
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})

	if err := ing.Ingest(ctx, "u1", "d1"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	got, _, _ := s.GetDocument(ctx, "u1", "d1")
	if got.Status != StatusEmbedded {
		t.Errorf("status = %q, want embedded", got.Status)
	}
	res, _ := s.Retrieve(ctx, "u1", nil, nil, unit(), 50)
	if len(res) == 0 {
		t.Error("no chunks retrievable after ingest")
	}
	if len(emb.gotInputs) == 0 {
		t.Error("embedder was never called")
	}
	if usage.userID != "u1" || usage.tokens == 0 || usage.requests != len(emb.gotInputs) || usage.calls != 1 {
		t.Errorf("embedding usage = user %q tokens %d requests %d calls %d, want one aggregate write for all batches", usage.userID, usage.tokens, usage.requests, usage.calls)
	}
}

func TestIngester_Ingest_extractionErrorMarksDocument(t *testing.T) {
	ing, s := newIngester(t,
		fakeExtractor{err: errors.New("tika down")},
		&fakeEmbedder{},
		fakeOpener{})
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})

	if err := ing.Ingest(ctx, "u1", "d1"); err == nil {
		t.Fatal("Ingest error = nil, want extraction error")
	}
	got, _, _ := s.GetDocument(ctx, "u1", "d1")
	if got.Status != StatusError || !strings.Contains(got.Error, "tika down") {
		t.Errorf("status=%q error=%q, want error containing 'tika down'", got.Status, got.Error)
	}
}

func TestIngester_Ingest_emptyExtractionMarksError(t *testing.T) {
	ing, s := newIngester(t,
		fakeExtractor{text: "   \n  "},
		&fakeEmbedder{},
		fakeOpener{})
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})

	if err := ing.Ingest(ctx, "u1", "d1"); err == nil {
		t.Fatal("Ingest error = nil, want error for empty extraction")
	}
	got, _, _ := s.GetDocument(ctx, "u1", "d1")
	if got.Status != StatusError {
		t.Errorf("status = %q, want error", got.Status)
	}
}

func TestStore_ResetStuckIngestions(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "a", UserID: "u1", VolumeRelpath: "files/a", Filename: "a", MIME: "text/plain", Status: StatusExtracting})
	_ = s.CreateDocument(ctx, Document{ID: "b", UserID: "u1", VolumeRelpath: "files/b", Filename: "b", MIME: "text/plain", Status: StatusEmbedding})
	_ = s.CreateDocument(ctx, Document{ID: "c", UserID: "u1", VolumeRelpath: "files/c", Filename: "c", MIME: "text/plain", Status: StatusEmbedded})

	if err := s.ResetStuckIngestions(ctx); err != nil {
		t.Fatalf("ResetStuckIngestions: %v", err)
	}
	for _, tc := range []struct{ id, want string }{{"a", StatusError}, {"b", StatusError}, {"c", StatusEmbedded}} {
		got, _, _ := s.GetDocument(ctx, "u1", tc.id)
		if got.Status != tc.want {
			t.Errorf("doc %s status = %q, want %q", tc.id, got.Status, tc.want)
		}
	}
}

func TestIngester_Ingest_cachesFullText(t *testing.T) {
	ing, s := newIngester(t,
		fakeExtractor{text: "the full extracted text of the document"},
		&fakeEmbedder{},
		fakeOpener{})
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})

	if err := ing.Ingest(ctx, "u1", "d1"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	cached, err := s.GetDocumentFullText(ctx, "u1", "d1")
	if err != nil {
		t.Fatalf("GetDocumentFullText: %v", err)
	}
	if cached != "the full extracted text of the document" {
		t.Errorf("cached full text = %q, want the extracted text", cached)
	}
}

func TestIngester_ExtractText_prefersCacheOverLiveExtraction(t *testing.T) {
	// An opener that always fails proves ExtractText returns the cached text without
	// touching the volume/extractor.
	ing, s := newIngester(t,
		fakeExtractor{text: "freshly extracted"},
		&fakeEmbedder{},
		fakeOpener{err: errors.New("volume unavailable")})
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusEmbedded})
	if err := s.SetDocumentFullText(ctx, "u1", "d1", "cached text"); err != nil {
		t.Fatalf("SetDocumentFullText: %v", err)
	}

	got, err := ing.ExtractText(ctx, "u1", "d1")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got != "cached text" {
		t.Errorf("ExtractText = %q, want cached text (no live extraction)", got)
	}
}

func TestIngester_ExtractText_fallsBackWhenNoCache(t *testing.T) {
	ing, s := newIngester(t,
		fakeExtractor{text: "freshly extracted"},
		&fakeEmbedder{},
		fakeOpener{})
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})

	got, err := ing.ExtractText(ctx, "u1", "d1")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got != "freshly extracted" {
		t.Errorf("ExtractText = %q, want live extraction when nothing cached", got)
	}
}
