package documents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/store"
)

type fakeIndexer struct{ called []string }

func (f *fakeIndexer) Ingest(_ context.Context, _, documentID string) error {
	f.called = append(f.called, documentID)
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
	return rag.EmbedResult{Vectors: out, Usage: rag.EmbeddingUsage{PromptTokens: len(inputs), TotalTokens: len(inputs), Present: true}}, nil
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
	}); !errors.Is(err, ErrChatDocumentLimit) {
		t.Fatalf("Upload overflow error = %v, want ErrChatDocumentLimit", err)
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
	}); !errors.Is(err, ErrChatDocumentLimit) {
		t.Fatalf("Upload overflow error = %v, want ErrChatDocumentLimit", err)
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

func (r *recordingUsage) AddEmbeddingUsage(_ context.Context, userID string, tokens, requests int) error {
	r.userID = userID
	r.tokens += tokens
	r.requests += requests
	return nil
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
