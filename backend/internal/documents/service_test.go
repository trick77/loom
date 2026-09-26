package documents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/store"
)

type fakeIndexer struct {
	called []string
	// entered is signalled when Ingest starts; block, when set, holds Ingest
	// open until it is closed or the context ends (mirroring a slow Tika or
	// embedding call).
	entered chan struct{}
	block   chan struct{}
	// ignoreCancel keeps Ingest blocked on block even after its context ends,
	// like a Tika call whose HTTP client does not honour the context.
	ignoreCancel bool
}

func (f *fakeIndexer) Ingest(ctx context.Context, _, documentID string) error {
	f.called = append(f.called, documentID)
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil && f.ignoreCancel {
		<-f.block
		return ctx.Err()
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeIndexer) ExtractText(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, inputs []string) (rag.EmbedResult, error) {
	out := make([][]float32, len(inputs))
	for i := range inputs {
		v := make([]float32, 1536)
		v[0] = 1
		out[i] = v
	}
	return rag.EmbedResult{Vectors: out, Usage: rag.EmbeddingUsage{PromptTokens: len(inputs), TotalTokens: len(inputs), Present: true, CostNanoUSD: 42 * int64(len(inputs)), CostPriced: true}}, nil
}

func newTestService(t *testing.T) (*Service, *fakeIndexer, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO threads (id, user_id, title) VALUES ('thread_1', 'u', 'Thread')`); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	usersDir := filepath.Join(dir, "users")
	idx := &fakeIndexer{}
	svc := NewService(rag.NewStore(db), artifact.NewStore(db), idx, fakeEmbedder{}, usersDir)
	return svc, idx, usersDir
}

func TestService_Upload_writesFileArtifactAndDocument(t *testing.T) {
	svc, _, usersDir := newTestService(t)
	ctx := context.Background()

	doc, art, err := svc.Upload(ctx, UploadInput{
		UserID:   "u",
		Filename: "Report.pdf",
		Reader:   strings.NewReader("PDF-BYTES"),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if art.Source != "user_uploaded" {
		t.Errorf("artifact source = %q, want user_uploaded", art.Source)
	}
	if doc.Status != rag.StatusPending {
		t.Errorf("doc status = %q, want pending", doc.Status)
	}
	if doc.ArtifactID == nil || *doc.ArtifactID != art.ID {
		t.Errorf("doc.ArtifactID = %v, want %q", doc.ArtifactID, art.ID)
	}
	abs := filepath.Join(usersDir, "u", "files", "Report.pdf")
	if data, err := os.ReadFile(abs); err != nil || string(data) != "PDF-BYTES" {
		t.Errorf("file at %s not written correctly: %q (err %v)", abs, data, err)
	}
}

func TestService_Upload_rejectsDisallowedFormat(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, _, err := svc.Upload(context.Background(), UploadInput{UserID: "u", Filename: "x.exe", Reader: strings.NewReader("x")}); err == nil {
		t.Fatal("Upload(.exe) error = nil, want rejection")
	}
}

func TestService_Upload_rejectsContentOverSizeLimit(t *testing.T) {
	svc, _, usersDir := newTestService(t)
	oversized := strings.NewReader(strings.Repeat("x", artifact.MaxArtifactSizeBytes+1))

	if _, _, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "u",
		Filename: "big.txt",
		Reader:   oversized,
	}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Upload(oversized) error = %v, want ErrTooLarge", err)
	}

	// The partially written file must be cleaned up, leaving no orphan on disk.
	leftover := 0
	_ = filepath.WalkDir(usersDir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			leftover++
		}
		return nil
	})
	if leftover != 0 {
		t.Fatalf("found %d leftover files after rejected oversized upload, want 0", leftover)
	}
}

func TestService_Upload_rejectsWhenThreadDocumentLimitReached(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	for i := 0; i < MaxChatDocuments; i++ {
		if _, _, err := svc.Upload(ctx, UploadInput{
			UserID:   "u",
			ThreadID: "thread_1",
			Filename: "doc" + string(rune('a'+i)) + ".txt",
			Reader:   strings.NewReader("hi"),
		}); err != nil {
			t.Fatalf("Upload #%d: %v", i+1, err)
		}
	}

	if _, _, err := svc.Upload(ctx, UploadInput{
		UserID:   "u",
		ThreadID: "thread_1",
		Filename: "overflow.txt",
		Reader:   strings.NewReader("hi"),
	}); !errors.Is(err, ErrThreadDocumentLimit) {
		t.Fatalf("Upload overflow error = %v, want ErrThreadDocumentLimit", err)
	}
}

func TestService_Upload_rejectsWhenThreadUploadLimitReachedByImages(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	for i := 0; i < MaxChatDocuments; i++ {
		if _, err := svc.artifacts.Create(ctx, artifact.CreateInput{
			UserID:          "u",
			ThreadID:        "thread_1",
			DisplayFilename: "image.png",
			VolumeRelPath:   "files/image.png",
			MIMEType:        "image/png",
			SizeBytes:       1,
			Source:          "user_uploaded",
		}); err != nil {
			t.Fatalf("seed image artifact #%d: %v", i+1, err)
		}
	}

	if _, _, err := svc.Upload(ctx, UploadInput{
		UserID:   "u",
		ThreadID: "thread_1",
		Filename: "overflow.txt",
		Reader:   strings.NewReader("hi"),
	}); !errors.Is(err, ErrThreadDocumentLimit) {
		t.Fatalf("Upload overflow error = %v, want ErrThreadDocumentLimit", err)
	}
}

func TestService_Index_delegatesToIndexer(t *testing.T) {
	svc, idx, _ := newTestService(t)
	ctx := context.Background()
	doc, _, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})
	if err := svc.Index(ctx, "u", doc.ID); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(idx.called) != 1 || idx.called[0] != doc.ID {
		t.Errorf("indexer called = %v, want [%s]", idx.called, doc.ID)
	}
}

func TestService_Delete_removesFileArtifactAndDocument(t *testing.T) {
	svc, _, usersDir := newTestService(t)
	ctx := context.Background()
	doc, art, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})

	if err := svc.Delete(ctx, "u", doc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := svc.Get(ctx, "u", doc.ID); ok {
		t.Error("document still present after delete")
	}
	if _, ok, _ := svc.artifacts.Get(ctx, "u", art.ID); ok {
		t.Error("artifact still present after delete")
	}
	if _, err := os.Stat(filepath.Join(usersDir, "u", "files", "a.txt")); !os.IsNotExist(err) {
		t.Errorf("file still on disk after delete (err %v)", err)
	}
}

type countingEmbedder struct{ calls int }

func (c *countingEmbedder) Embed(_ context.Context, inputs []string) (rag.EmbedResult, error) {
	c.calls++
	out := make([][]float32, len(inputs))
	for i := range inputs {
		v := make([]float32, 1536)
		v[0] = 1
		out[i] = v
	}
	return rag.EmbedResult{Vectors: out, Usage: rag.EmbeddingUsage{PromptTokens: 9, TotalTokens: 9, Present: true}}, nil
}

type recordingUsage struct {
	userID   string
	tokens   int
	requests int
}

func (r *recordingUsage) AddEmbeddingUsage(_ context.Context, userID string, tokens, requests int, _ int64) error {
	r.userID = userID
	r.tokens += tokens
	r.requests += requests
	return nil
}

// The query embedding is a call of the chat turn, so its cost reaches the
// turn's accumulator (the thread's Σ) — as rolled-up cost, because it is
// already in the user's embedding totals.
func TestService_Retrieve_recordsQueryEmbeddingCostOnTheTurn(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.SetUsageRecorder(&recordingUsage{})
	ctx := context.Background()
	doc, _, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})
	v := make([]float32, 1536)
	v[0] = 1
	if err := svc.store.ReplaceChunks(ctx, "u", doc.ID, []rag.TextChunk{{Text: "hello"}}, [][]float32{v}); err != nil {
		t.Fatalf("seed chunks: %v", err)
	}
	acc := llm.NewUsageAccumulator()
	if _, err := svc.Retrieve(llm.WithUsageAccumulator(ctx, acc), "u", nil, nil, "what is hello", 5); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if nano, _ := acc.TurnCost(); nano != 42 {
		t.Fatalf("turn cost = %d, want 42", nano)
	}
	if nano, _ := acc.Cost(); nano != 0 {
		t.Fatalf("rollup cost = %d, want 0 (already in the embedding totals)", nano)
	}
}

func TestService_Retrieve_skipsEmbeddingWhenNoChunks(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`); err != nil {
		t.Fatal(err)
	}
	emb := &countingEmbedder{}
	svc := NewService(rag.NewStore(db), artifact.NewStore(db), &fakeIndexer{}, emb, filepath.Join(dir, "users"))

	res, err := svc.Retrieve(context.Background(), "u", nil, nil, "anything", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("Retrieve = %d, want 0 with nothing indexed", len(res))
	}
	if emb.calls != 0 {
		t.Errorf("embedder called %d times, want 0 (guarded)", emb.calls)
	}
}

func TestService_Retrieve_embedsQueryAndReturnsChunks(t *testing.T) {
	svc, _, _ := newTestService(t)
	usage := &recordingUsage{}
	svc.SetUsageRecorder(usage)
	ctx := context.Background()
	doc, _, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})
	// Directly index chunks via the store to avoid Tika in this unit test.
	v := make([]float32, 1536)
	v[0] = 1
	if err := svc.store.ReplaceChunks(ctx, "u", doc.ID, []rag.TextChunk{{Text: "hello"}}, [][]float32{v}); err != nil {
		t.Fatalf("seed chunks: %v", err)
	}
	res, err := svc.Retrieve(ctx, "u", nil, nil, "what is hello", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(res) != 1 || res[0].Text != "hello" {
		t.Errorf("Retrieve = %+v, want one chunk 'hello'", res)
	}
	if usage.userID != "u" || usage.tokens != 1 || usage.requests != 1 {
		t.Errorf("embedding usage = user %q tokens %d requests %d, want u/1/1", usage.userID, usage.tokens, usage.requests)
	}
}

// A second index request for a document that is already being ingested (a
// double click, two tabs) must not start a second Tika/embedding run, and an
// unindex or delete must not race the running ingest's chunk writes.
func TestService_Index_isSingleFlightPerDocument(t *testing.T) {
	svc, idx, _ := newTestService(t)
	idx.entered = make(chan struct{}, 1)
	idx.block = make(chan struct{})
	ctx := context.Background()
	doc, _, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})

	first := make(chan error, 1)
	go func() { first <- svc.Index(ctx, "u", doc.ID) }()
	<-idx.entered

	if err := svc.Index(ctx, "u", doc.ID); !errors.Is(err, ErrIndexInProgress) {
		t.Fatalf("second Index() error = %v, want ErrIndexInProgress", err)
	}
	if err := svc.Unindex(ctx, "u", doc.ID); !errors.Is(err, ErrIndexInProgress) {
		t.Fatalf("Unindex() during ingest error = %v, want ErrIndexInProgress", err)
	}
	close(idx.block)
	if err := <-first; err != nil {
		t.Fatalf("first Index() error = %v", err)
	}
	if len(idx.called) != 1 {
		t.Fatalf("Ingest calls = %d, want 1", len(idx.called))
	}
	// Once the ingest is done the document is free again.
	if err := svc.Unindex(ctx, "u", doc.ID); err != nil {
		t.Fatalf("Unindex() after ingest error = %v", err)
	}
}

// An ingest that never returns (a hung sidecar) used to leave the document
// stuck in extracting/embedding until restart; it is bounded, and the failure
// is recorded even though the ingest's own context is dead by then.
func TestService_Index_timesOutAndRecordsFailure(t *testing.T) {
	svc, idx, _ := newTestService(t)
	idx.block = make(chan struct{})
	defer close(idx.block)
	svc.SetIndexTimeout(20 * time.Millisecond)
	ctx := context.Background()
	doc, _, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})

	if err := svc.Index(ctx, "u", doc.ID); err == nil {
		t.Fatal("Index() error = nil, want a timeout")
	}
	got, ok, err := svc.Get(ctx, "u", doc.ID)
	if err != nil || !ok {
		t.Fatalf("Get() = ok %v, err %v", ok, err)
	}
	if got.Status != rag.StatusError || got.Error == "" {
		t.Fatalf("status after timeout = %q (%q), want error with a reason", got.Status, got.Error)
	}
}

// A delete during an ingest cancels the run and waits for it instead of
// refusing: the composer deletes a removed attachment seconds after its upload
// started indexing, and a 409 there orphaned the document in the thread.
func TestService_Delete_cancelsRunningIngest(t *testing.T) {
	svc, idx, _ := newTestService(t)
	idx.entered = make(chan struct{}, 1)
	idx.block = make(chan struct{})
	ctx := context.Background()
	doc, _, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})

	first := make(chan error, 1)
	go func() { first <- svc.Index(ctx, "u", doc.ID) }()
	<-idx.entered

	if err := svc.Delete(ctx, "u", doc.ID); err != nil {
		t.Fatalf("Delete() during ingest error = %v, want nil", err)
	}
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Index() error = %v, want context.Canceled", err)
	}
	if _, ok, err := svc.store.GetDocument(ctx, "u", doc.ID); err != nil || ok {
		t.Fatalf("document still present after Delete() (ok=%v, err=%v)", ok, err)
	}
	if svc.indexing("u", doc.ID) {
		t.Fatal("inflight key still held after the cancelled ingest")
	}
}

// An ingest that ignores cancellation does not hang the delete: after the
// bounded wait the caller gets ErrIndexInProgress (a 409 it can retry).
func TestService_Delete_givesUpOnAStuckIngest(t *testing.T) {
	svc, idx, _ := newTestService(t)
	svc.cancelWait = 20 * time.Millisecond
	idx.entered = make(chan struct{}, 1)
	idx.block = make(chan struct{})
	idx.ignoreCancel = true
	ctx := context.Background()
	doc, _, _ := svc.Upload(ctx, UploadInput{UserID: "u", Filename: "a.txt", Reader: strings.NewReader("hi")})

	first := make(chan error, 1)
	go func() { first <- svc.Index(ctx, "u", doc.ID) }()
	<-idx.entered

	if err := svc.Delete(ctx, "u", doc.ID); !errors.Is(err, ErrIndexInProgress) {
		t.Fatalf("Delete() on a stuck ingest error = %v, want ErrIndexInProgress", err)
	}
	close(idx.block)
	<-first
}
