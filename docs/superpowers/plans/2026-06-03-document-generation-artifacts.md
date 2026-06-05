# Document Generation Artifacts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AnythingLLM-style assistant-generated files to Slopr: chat tools can create text, PDF, DOCX, XLSX, and PPTX artifacts, store them in the user's Artifacts volume, and render downloadable chat cards.

**Architecture:** Keep generation as built-in Slopr tools rather than MCP servers, because these tools need the authenticated user, current thread/project scope, and direct access to the per-user volume sandbox. Add artifact metadata to SQLite, route downloads through artifact ids, emit a new SSE `artifact` event during chat streaming, and persist artifact metadata with assistant messages so old chats re-render cards. Start with text artifacts and the plumbing, then add XLSX/PDF/DOCX/PPTX generators behind the same interface.

**Tech Stack:** Go 1.25, stdlib `net/http`, SQLite migrations, existing SSE, React 19 + TypeScript, Vitest, Go `archive/zip` OOXML writers, `github.com/xuri/excelize/v2` for XLSX, `github.com/signintech/gopdf` for PDF.

---

## File Structure

- Create `backend/internal/artifact/model.go`: artifact metadata types, MIME mapping, and limits.
- Create `backend/internal/artifact/path.go`: user-volume path resolver, filename sanitization, collision handling, and symlink-safe confinement.
- Create `backend/internal/artifact/store.go`: SQLite artifact persistence and user-scoped lookup.
- Create `backend/internal/artifact/path_test.go`: path and filename security tests.
- Create `backend/internal/artifact/store_test.go`: metadata persistence tests.
- Create `backend/internal/store/migrations/0008_artifacts.sql`: `artifacts` table plus `messages.artifacts`.
- Modify `backend/internal/chat/model.go`: add `Artifacts json.RawMessage`.
- Modify `backend/internal/chat/message_store.go`: add `AddMessageWithArtifacts`.
- Modify `backend/internal/chat/scan.go`: scan artifacts JSON.
- Modify `backend/internal/chat/store_test.go`: cover message artifact persistence.
- Create `backend/internal/docgen/model.go`: generator interfaces and requests.
- Create `backend/internal/docgen/text.go`: text-like artifact generator.
- Create `backend/internal/docgen/text_test.go`: text generator tests.
- Create `backend/internal/docgen/xlsx.go`: XLSX generator.
- Create `backend/internal/docgen/xlsx_test.go`: XLSX smoke tests.
- Create `backend/internal/docgen/pdf.go`: PDF generator.
- Create `backend/internal/docgen/pdf_test.go`: PDF smoke tests.
- Create `backend/internal/docgen/docx.go`: deterministic DOCX OOXML generator.
- Create `backend/internal/docgen/docx_test.go`: DOCX smoke tests.
- Create `backend/internal/docgen/pptx.go`: deterministic PPTX OOXML generator.
- Create `backend/internal/docgen/pptx_test.go`: PPTX smoke tests.
- Create `backend/internal/docgen/ooxml.go`: shared ZIP/XML helpers for DOCX/PPTX.
- Create `backend/internal/httpapi/artifact_handlers.go`: authenticated artifact download endpoint.
- Modify `backend/internal/httpapi/server.go`: wire artifact store/generator dependencies and `GET /api/artifacts/{artifactID}/download`.
- Modify `backend/internal/httpapi/chat_types.go`: add artifact SSE/JSON response types.
- Modify `backend/internal/httpapi/message_stream_handlers.go`: merge built-in docgen tools into tool list, execute built-in tools, stream artifact events, and persist assistant message artifacts.
- Modify `backend/internal/httpapi/chat_test_helpers_test.go`: fake artifact/docgen dependencies.
- Modify `backend/internal/httpapi/message_stream_handlers_test.go`: built-in tool and SSE tests.
- Modify `backend/internal/httpapi/server_test.go`: artifact download auth tests.
- Modify `backend/cmd/slopr/main.go`: construct artifact store and generators from `SLOPR_USERS_DIR`.
- Modify `backend/go.mod` and `backend/go.sum`: add generation dependencies.
- Modify `frontend/src/api.ts`: add artifact types, SSE handler, and download helper.
- Modify `frontend/src/api.test.ts`: cover artifact SSE parsing and download helper.
- Modify `frontend/src/ChatShell.tsx`: render live and persisted artifact cards.
- Modify `frontend/src/App.test.tsx`: cover artifact card behavior.

## Task 1: Artifact Metadata and Safe Output Paths

**Files:**
- Create: `backend/internal/artifact/model.go`
- Create: `backend/internal/artifact/path.go`
- Create: `backend/internal/artifact/path_test.go`

- [ ] **Step 1: Write failing path tests**

Create `backend/internal/artifact/path_test.go`:

```go
package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOutputPathUsesThreadScope(t *testing.T) {
	root := t.TempDir()
	req := OutputRequest{
		UsersDir:         root,
		UserID:           "user_1",
		ThreadID:         "thread_1",
		ProjectID:        strPtr("proj_1"),
		DisplayFilename:  "Q1 report.pdf",
		Extension:        "pdf",
		ProjectlessFiles: false,
	}

	out, err := ResolveOutputPath(req)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if out.VolumeRelPath != "projects/proj_1/outputs/Q1 report.pdf" {
		t.Fatalf("VolumeRelPath = %q", out.VolumeRelPath)
	}
	if filepath.Dir(out.AbsPath) != filepath.Join(root, "user_1", "projects", "proj_1", "outputs") {
		t.Fatalf("AbsPath directory = %q", filepath.Dir(out.AbsPath))
	}
}

func TestResolveOutputPathUsesProjectlessOutputs(t *testing.T) {
	root := t.TempDir()
	out, err := ResolveOutputPath(OutputRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "notes.md",
		Extension:       "md",
	})
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if out.VolumeRelPath != "files/outputs/notes.md" {
		t.Fatalf("VolumeRelPath = %q", out.VolumeRelPath)
	}
}

func TestResolveOutputPathRejectsTraversalAndReservedPaths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../secret.pdf", "/tmp/secret.pdf", ".slopr/secret.pdf", "folder/../../secret.pdf"} {
		_, err := ResolveOutputPath(OutputRequest{
			UsersDir:        root,
			UserID:          "user_1",
			ThreadID:        "thread_1",
			DisplayFilename: name,
			Extension:       "pdf",
		})
		if err == nil {
			t.Fatalf("ResolveOutputPath(%q) succeeded, want error", name)
		}
	}
}

func TestResolveOutputPathAddsCollisionSuffix(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "user_1", "files", "outputs")
	if err := os.MkdirAll(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "report.pdf"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := ResolveOutputPath(OutputRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "report.pdf",
		Extension:       "pdf",
	})
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if out.DisplayFilename != "report-2.pdf" {
		t.Fatalf("DisplayFilename = %q", out.DisplayFilename)
	}
}

func TestResolveOutputPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outputs := filepath.Join(root, "user_1", "files", "outputs")
	if err := os.MkdirAll(outputs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(outputs, "escape")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveExisting(root, "user_1", "files/outputs/escape/report.pdf")
	if err == nil {
		t.Fatal("ResolveExisting through symlink succeeded, want error")
	}
}

func strPtr(value string) *string {
	return &value
}
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd backend && go test ./internal/artifact`

Expected: fail because `backend/internal/artifact` does not exist.

- [ ] **Step 3: Add artifact types**

Create `backend/internal/artifact/model.go`:

```go
package artifact

import "time"

const (
	MaxDisplayFilenameLength = 180
	MaxArtifactSizeBytes     = 25 << 20
)

type Artifact struct {
	ID              string     `json:"id"`
	UserID          string     `json:"-"`
	ThreadID        string     `json:"threadId"`
	ProjectID       *string    `json:"projectId,omitempty"`
	DisplayFilename string     `json:"displayFilename"`
	VolumeRelPath   string     `json:"-"`
	MIMEType        string     `json:"mimeType"`
	SizeBytes       int64      `json:"sizeBytes"`
	Source          string     `json:"source"`
	CreatedAt       time.Time  `json:"createdAt"`
	DownloadURL     string     `json:"downloadUrl"`
}

type OutputRequest struct {
	UsersDir         string
	UserID           string
	ThreadID         string
	ProjectID        *string
	DisplayFilename  string
	Extension        string
	ProjectlessFiles bool
}

type OutputPath struct {
	AbsPath         string
	VolumeRelPath   string
	DisplayFilename string
	MIMEType        string
}

func MIMEType(extension string) string {
	switch normalizeExtension(extension) {
	case "pdf":
		return "application/pdf"
	case "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "csv":
		return "text/csv; charset=utf-8"
	case "html":
		return "text/html; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "xml":
		return "application/xml; charset=utf-8"
	case "md":
		return "text/markdown; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
```

- [ ] **Step 4: Implement safe path resolution**

Create `backend/internal/artifact/path.go`:

```go
package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var safeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func ResolveOutputPath(req OutputRequest) (OutputPath, error) {
	if strings.TrimSpace(req.UsersDir) == "" {
		return OutputPath{}, errors.New("users dir is required")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return OutputPath{}, errors.New("user id is required")
	}
	extension := normalizeExtension(req.Extension)
	if extension == "" {
		return OutputPath{}, errors.New("extension is required")
	}
	display, err := sanitizeDisplayFilename(req.DisplayFilename, extension)
	if err != nil {
		return OutputPath{}, err
	}

	baseRel := filepath.Join("files", "outputs")
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		baseRel = filepath.Join("projects", *req.ProjectID, "outputs")
	}

	userRoot := filepath.Join(req.UsersDir, req.UserID)
	outputDir := filepath.Join(userRoot, baseRel)
	if err := ensureInside(userRoot, outputDir); err != nil {
		return OutputPath{}, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return OutputPath{}, fmt.Errorf("create output directory: %w", err)
	}
	finalName, finalAbs := collisionFreeName(outputDir, display)
	rel := filepath.ToSlash(filepath.Join(baseRel, finalName))
	return OutputPath{
		AbsPath:         finalAbs,
		VolumeRelPath:   rel,
		DisplayFilename: finalName,
		MIMEType:        MIMEType(extension),
	}, nil
}

func ResolveExisting(usersDir, userID, volumeRelPath string) (string, error) {
	if filepath.IsAbs(volumeRelPath) || strings.Contains(volumeRelPath, "..") {
		return "", errors.New("invalid artifact path")
	}
	if strings.HasPrefix(filepath.ToSlash(volumeRelPath), ".slopr/") {
		return "", errors.New("reserved artifact path")
	}
	userRoot := filepath.Join(usersDir, userID)
	abs := filepath.Join(userRoot, filepath.FromSlash(volumeRelPath))
	if err := ensureInside(userRoot, abs); err != nil {
		return "", err
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("resolve artifact parent: %w", err)
	}
	if err := ensureInside(userRoot, resolvedParent); err != nil {
		return "", err
	}
	return abs, nil
}

func sanitizeDisplayFilename(input, extension string) (string, error) {
	name := strings.TrimSpace(filepath.Base(input))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "artifact." + extension
	}
	name = strings.ReplaceAll(name, "\x00", "")
	name = safeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, " .")
	if name == "" {
		name = "artifact." + extension
	}
	if filepath.IsAbs(input) || strings.Contains(filepath.ToSlash(input), "../") || strings.HasPrefix(filepath.ToSlash(input), ".slopr/") {
		return "", errors.New("invalid filename")
	}
	if !strings.EqualFold(filepath.Ext(name), "."+extension) {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + "." + extension
	}
	if len(name) > MaxDisplayFilenameLength {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		name = stem[:MaxDisplayFilenameLength-len(ext)] + ext
	}
	return name, nil
}

func collisionFreeName(dir, name string) (string, string) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	candidate := name
	for i := 2; ; i++ {
		abs := filepath.Join(dir, candidate)
		if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			return candidate, abs
		}
		candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
	}
}

func ensureInside(root, path string) error {
	rootClean, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathClean, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootClean, pathClean)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return errors.New("path escapes user root")
	}
	return nil
}

func normalizeExtension(extension string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(extension), "."))
}
```

- [ ] **Step 5: Run tests and verify pass**

Run: `cd backend && go test ./internal/artifact`

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/artifact/model.go backend/internal/artifact/path.go backend/internal/artifact/path_test.go
git commit -m "feat: add artifact path sandbox"
```

## Task 2: Artifact Persistence and Message Metadata

**Files:**
- Create: `backend/internal/store/migrations/0008_artifacts.sql`
- Create: `backend/internal/artifact/store.go`
- Create: `backend/internal/artifact/store_test.go`
- Modify: `backend/internal/chat/model.go`
- Modify: `backend/internal/chat/message_store.go`
- Modify: `backend/internal/chat/scan.go`
- Modify: `backend/internal/chat/store_test.go`

- [ ] **Step 1: Add failing artifact store tests**

Create `backend/internal/artifact/store_test.go`:

```go
package artifact

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/trick77/slopr/internal/store"
)

func TestStoreCreatesAndFindsArtifactByUser(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewStore(db)
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(context.Background(), CreateInput{
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "report.pdf",
		VolumeRelPath:   "files/outputs/report.pdf",
		MIMEType:        "application/pdf",
		SizeBytes:       12,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, ok, err := s.Get(context.Background(), "user_1", created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false")
	}
	if found.DisplayFilename != "report.pdf" || found.DownloadURL == "" {
		t.Fatalf("found = %#v", found)
	}

	if _, ok, err := s.Get(context.Background(), "user_2", created.ID); err != nil || ok {
		t.Fatalf("cross-user Get() ok=%v err=%v, want false nil", ok, err)
	}
}
```

- [ ] **Step 2: Add failing message metadata test**

Append to `backend/internal/chat/store_test.go`:

```go
func TestMessagesPersistArtifacts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	userID := "user_1"
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Artifacts"})
	if err != nil {
		t.Fatal(err)
	}

	rawArtifacts := json.RawMessage(`[{"id":"art_1","displayFilename":"report.pdf","downloadUrl":"/api/artifacts/art_1/download"}]`)
	message, err := store.AddMessageWithArtifacts(ctx, userID, thread.ID, RoleAssistant, "Created report.pdf", MessageTokenUsage{}, rawArtifacts)
	if err != nil {
		t.Fatalf("AddMessageWithArtifacts() error = %v", err)
	}
	if string(message.Artifacts) != string(rawArtifacts) {
		t.Fatalf("message.Artifacts = %s", message.Artifacts)
	}

	messages, found, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	if string(messages[0].Artifacts) != string(rawArtifacts) {
		t.Fatalf("listed Artifacts = %s", messages[0].Artifacts)
	}
}
```

Add `encoding/json` to the imports in `backend/internal/chat/store_test.go`.

- [ ] **Step 3: Run tests and verify failure**

Run: `cd backend && go test ./internal/artifact ./internal/chat`

Expected: fail with missing artifact store and missing `AddMessageWithArtifacts`.

- [ ] **Step 4: Add migration**

Create `backend/internal/store/migrations/0008_artifacts.sql`:

```sql
CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id TEXT NOT NULL,
    project_id TEXT,
    display_filename TEXT NOT NULL,
    volume_relpath TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    source TEXT NOT NULL DEFAULT 'assistant_generated',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_artifacts_user_created ON artifacts(user_id, created_at DESC);
CREATE INDEX idx_artifacts_thread ON artifacts(user_id, thread_id, created_at DESC);

ALTER TABLE messages ADD COLUMN artifacts TEXT NOT NULL DEFAULT '[]';
```

- [ ] **Step 5: Implement artifact store**

Create `backend/internal/artifact/store.go`:

```go
package artifact

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/trick77/slopr/internal/chat"
)

type Store struct {
	db *sql.DB
}

type CreateInput struct {
	UserID          string
	ThreadID        string
	ProjectID       *string
	DisplayFilename string
	VolumeRelPath   string
	MIMEType        string
	SizeBytes       int64
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, in CreateInput) (Artifact, error) {
	id := chat.NewIDForInternalUse()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.UserID, in.ThreadID, in.ProjectID, in.DisplayFilename, in.VolumeRelPath, in.MIMEType, in.SizeBytes,
	)
	if err != nil {
		return Artifact{}, fmt.Errorf("insert artifact: %w", err)
	}
	artifact, ok, err := s.Get(ctx, in.UserID, id)
	if err != nil {
		return Artifact{}, err
	}
	if !ok {
		return Artifact{}, fmt.Errorf("inserted artifact not found")
	}
	return artifact, nil
}

func (s *Store) Get(ctx context.Context, userID, artifactID string) (Artifact, bool, error) {
	var out Artifact
	var createdAt string
	var projectID sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at
FROM artifacts
WHERE user_id = ? AND id = ?`, userID, artifactID).Scan(
		&out.ID, &out.UserID, &out.ThreadID, &projectID, &out.DisplayFilename,
		&out.VolumeRelPath, &out.MIMEType, &out.SizeBytes, &out.Source, &createdAt,
	)
	if err == sql.ErrNoRows {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, fmt.Errorf("get artifact: %w", err)
	}
	if projectID.Valid {
		out.ProjectID = &projectID.String
	}
	parsed, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		return Artifact{}, false, fmt.Errorf("parse artifact created_at: %w", err)
	}
	out.CreatedAt = parsed
	out.DownloadURL = "/api/artifacts/" + out.ID + "/download"
	return out, true, nil
}
```

Add this exported helper to `backend/internal/chat/id.go`:

```go
func NewIDForInternalUse() string {
	return newID()
}
```

- [ ] **Step 6: Add message artifact metadata**

In `backend/internal/chat/model.go`, add to `Message`:

```go
Artifacts json.RawMessage `json:"artifacts"`
```

In `backend/internal/chat/message_store.go`, change `AddMessageWithUsage` to delegate:

```go
func (s *Store) AddMessageWithUsage(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage) (Message, error) {
	return s.AddMessageWithArtifacts(ctx, userID, threadID, role, content, usage, nil)
}

func (s *Store) AddMessageWithArtifacts(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage) (Message, error) {
	if len(artifacts) == 0 {
		artifacts = json.RawMessage("[]")
	}
	if !json.Valid(artifacts) {
		return Message{}, errors.New("message artifacts must be valid JSON")
	}
```

Update the insert statement to include `artifacts`:

```sql
INSERT INTO messages (
    id,
    thread_id,
    user_id,
    role,
    content,
    reasoning_content,
    tool_calls,
    citations,
    artifacts,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens,
    duration_ms,
    model,
    reasoning_effort
)
VALUES (?, ?, ?, ?, ?, ?, '[]', '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?)
```

Pass `string(artifacts)` before usage token fields.

Update message selects in `ListMessages` and `getMessage` to select `artifacts` after `citations`.

In `backend/internal/chat/scan.go`, scan a new `artifacts string` value after citations and assign:

```go
message.Artifacts = defaultJSON(artifacts)
```

- [ ] **Step 7: Run tests and verify pass**

Run: `cd backend && go test ./internal/artifact ./internal/chat ./internal/store`

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/store/migrations/0008_artifacts.sql backend/internal/artifact/store.go backend/internal/artifact/store_test.go backend/internal/chat/model.go backend/internal/chat/message_store.go backend/internal/chat/scan.go backend/internal/chat/store_test.go backend/internal/chat/id.go
git commit -m "feat: persist generated artifact metadata"
```

## Task 3: Text Generator and Atomic Artifact Writes

**Files:**
- Create: `backend/internal/docgen/model.go`
- Create: `backend/internal/docgen/text.go`
- Create: `backend/internal/docgen/text_test.go`

- [ ] **Step 1: Write failing text generator tests**

Create `backend/internal/docgen/text_test.go`:

```go
package docgen

import (
	"bytes"
	"testing"
)

func TestTextGeneratorWritesUTF8Content(t *testing.T) {
	gen := TextGenerator{}
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{
		Format:   "text",
		Filename: "notes.md",
		Payload: map[string]any{
			"content":   "# Hello\n\nGruezi",
			"extension": "md",
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if out.String() != "# Hello\n\nGruezi" {
		t.Fatalf("output = %q", out.String())
	}
	if meta.Extension != "md" || meta.MIMEType != "text/markdown; charset=utf-8" {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestTextGeneratorRejectsOversizeContent(t *testing.T) {
	gen := TextGenerator{MaxInputBytes: 4}
	_, err := gen.Generate(GenerateRequest{
		Filename: "notes.txt",
		Payload:  map[string]any{"content": "12345"},
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd backend && go test ./internal/docgen`

Expected: fail because package does not exist.

- [ ] **Step 3: Add generator interfaces**

Create `backend/internal/docgen/model.go`:

```go
package docgen

import "io"

const MaxGeneratedInputBytes = 1 << 20

type GenerateRequest struct {
	Format   string
	Filename string
	Payload  map[string]any
}

type GeneratedMeta struct {
	DisplayFilename string
	Extension       string
	MIMEType        string
}

type Generator interface {
	ToolName() string
	Schema() ToolSchema
	Generate(GenerateRequest, io.Writer) (GeneratedMeta, error)
}

type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}
```

- [ ] **Step 4: Implement text generator**

Create `backend/internal/docgen/text.go`:

```go
package docgen

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

type TextGenerator struct {
	MaxInputBytes int
}

func (g TextGenerator) ToolName() string {
	return "create_text_file"
}

func (g TextGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create a UTF-8 text-like file such as txt, md, csv, json, html, xml, yaml, or log.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename":  map[string]any{"type": "string"},
				"extension": map[string]any{"type": "string"},
				"content":   map[string]any{"type": "string"},
			},
			"required":             []string{"filename", "content"},
			"additionalProperties": false,
		},
	}
}

func (g TextGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	content, ok := req.Payload["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content is required")
	}
	limit := g.MaxInputBytes
	if limit == 0 {
		limit = MaxGeneratedInputBytes
	}
	if len(content) > limit {
		return GeneratedMeta{}, fmt.Errorf("content is too large")
	}
	extension := "txt"
	if raw, ok := req.Payload["extension"].(string); ok && strings.TrimSpace(raw) != "" {
		extension = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
	}
	if _, err := io.WriteString(w, content); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{
		DisplayFilename: req.Filename,
		Extension:       extension,
		MIMEType:        artifact.MIMEType(extension),
	}, nil
}
```

- [ ] **Step 5: Run tests and verify pass**

Run: `cd backend && go test ./internal/docgen`

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/docgen/model.go backend/internal/docgen/text.go backend/internal/docgen/text_test.go
git commit -m "feat: add text artifact generator"
```

## Task 4: Built-In Document Tools in the Chat Loop

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/chat_types.go`
- Modify: `backend/internal/httpapi/message_stream_handlers.go`
- Modify: `backend/internal/httpapi/chat_test_helpers_test.go`
- Modify: `backend/internal/httpapi/message_stream_handlers_test.go`
- Modify: `backend/cmd/slopr/main.go`

- [ ] **Step 1: Add failing stream test for generated text artifact**

Add these imports to `backend/internal/httpapi/message_stream_handlers_test.go` if they are not already present:

```go
	"path/filepath"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/docgen"
	"github.com/trick77/slopr/internal/store"
```

Append this test to `backend/internal/httpapi/message_stream_handlers_test.go`:

```go
func TestStreamMessageExecutesBuiltInArtifactTool(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	chatStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	user := testUser
	thread, err := chatStore.CreateThread(context.Background(), user.ID, chat.CreateThreadInput{Title: "Artifacts"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "create_text_file",
						Arguments: `{"filename":"notes.md","extension":"md","content":"# Notes"}`,
					},
				}},
			},
			{Content: "Created notes.md."},
		},
	}
	server := newAuthenticatedChatServer(t, Deps{
		Chat:      chatStore,
		Artifacts: artifactStore,
		DocTools:  []docgen.Generator{docgen.TextGenerator{}},
		UsersDir:  t.TempDir(),
		LLM:       llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"make a markdown file"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: artifact") {
		t.Fatalf("stream missing artifact event:\n%s", rec.Body.String())
	}

	messages, found, err := chatStore.ListMessages(context.Background(), user.ID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	last := messages[len(messages)-1]
	if !strings.Contains(string(last.Artifacts), "notes.md") {
		t.Fatalf("assistant artifacts = %s", last.Artifacts)
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run: `cd backend && go test ./internal/httpapi -run TestStreamMessageExecutesBuiltInArtifactTool`

Expected: fail because `Deps.Artifacts`, `Deps.DocTools`, `Deps.UsersDir`, and built-in execution do not exist.

- [ ] **Step 3: Add HTTP API dependency interfaces**

In `backend/internal/httpapi/server.go`, add imports:

```go
	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/docgen"
```

Add to `Deps`:

```go
	Artifacts ArtifactStore
	DocTools  []docgen.Generator
	UsersDir  string
```

Add to `server`:

```go
	artifacts *artifact.Store
	docTools  []docgen.Generator
	usersDir  string
```

Add the interface:

```go
type ArtifactStore interface {
	Create(context.Context, artifact.CreateInput) (artifact.Artifact, error)
	Get(context.Context, string, string) (artifact.Artifact, bool, error)
}
```

Assign the fields in `New`.

- [ ] **Step 4: Add artifact response type**

In `backend/internal/httpapi/chat_types.go`, add:

```go
type artifactResponse struct {
	ID              string  `json:"id"`
	DisplayFilename string  `json:"displayFilename"`
	MIMEType        string  `json:"mimeType"`
	SizeBytes       int64   `json:"sizeBytes"`
	ProjectID       *string `json:"projectId,omitempty"`
	DownloadURL     string  `json:"downloadUrl"`
}
```

- [ ] **Step 5: Merge built-in tools with MCP tools**

In `message_stream_handlers.go`, add:

```go
type assistantLoopResult struct {
	llm.StreamResult
	Artifacts []artifactResponse
}
```

Change `runAssistantLoop` to return `assistantLoopResult`. Add:

```go
func (s *server) availableTools() []llm.Tool {
	tools := []llm.Tool(nil)
	for _, gen := range s.docTools {
		schema := gen.Schema()
		tools = append(tools, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        schema.Name,
				Description: schema.Description,
				Parameters:  schema.Parameters,
			},
		})
	}
	if s.mcp != nil {
		tools = append(tools, s.mcp.Tools()...)
	}
	return tools
}
```

Use `tools := s.availableTools()` instead of `s.mcp.Tools()`.

- [ ] **Step 6: Execute built-in artifact tools**

Add a helper in `message_stream_handlers.go`:

```go
func (s *server) executeBuiltInTool(ctx context.Context, stream *sse.Writer, user auth.User, thread chat.Thread, call llm.ToolCall) (string, *artifactResponse, bool) {
	var generator docgen.Generator
	for _, candidate := range s.docTools {
		if candidate.ToolName() == call.Function.Name {
			generator = candidate
			break
		}
	}
	if generator == nil {
		return "", nil, false
	}
	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return capToolOutput("tool failed: invalid arguments: "+err.Error()), nil, true
	}
	filename, _ := args["filename"].(string)
	var buffer bytes.Buffer
	meta, err := generator.Generate(docgen.GenerateRequest{
		Format:   generator.ToolName(),
		Filename: filename,
		Payload:  args,
	}, &buffer)
	if err != nil {
		return capToolOutput("tool failed: "+err.Error()), nil, true
	}
	if buffer.Len() > artifact.MaxArtifactSizeBytes {
		return "tool failed: generated file is too large", nil, true
	}
	out, err := artifact.ResolveOutputPath(artifact.OutputRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: meta.DisplayFilename,
		Extension:       meta.Extension,
	})
	if err != nil {
		return capToolOutput("tool failed: "+err.Error()), nil, true
	}
	if err := os.WriteFile(out.AbsPath, buffer.Bytes(), 0o600); err != nil {
		return capToolOutput("tool failed: write artifact: "+err.Error()), nil, true
	}
	created, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: out.DisplayFilename,
		VolumeRelPath:   out.VolumeRelPath,
		MIMEType:        out.MIMEType,
		SizeBytes:       int64(buffer.Len()),
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		return capToolOutput("tool failed: persist artifact: "+err.Error()), nil, true
	}
	response := artifactResponse{
		ID:              created.ID,
		DisplayFilename: created.DisplayFilename,
		MIMEType:        created.MIMEType,
		SizeBytes:       created.SizeBytes,
		ProjectID:       created.ProjectID,
		DownloadURL:     created.DownloadURL,
	}
	_ = sendSSEJSON(stream, "artifact", response)
	return fmt.Sprintf("created artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), &response, true
}
```

Add imports `bytes` and `os`.

- [ ] **Step 7: Persist assistant artifacts**

In `runAssistantLoop`, keep a local `artifacts []artifactResponse` and append any response returned
by `executeBuiltInTool`. Return `assistantLoopResult{StreamResult: result, Artifacts: artifacts}`.

In `handleStreamMessage`, marshal `assistantResult.Artifacts` and call:

```go
assistantMessage, err := s.chat.AddMessageWithArtifacts(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent, messageMetricsFromResult(assistantResult.StreamResult), artifactsJSON)
```

Send `assistant_message` after artifacts have already streamed so the frontend can reconcile the
final persisted message.

- [ ] **Step 8: Wire production dependencies**

In `backend/cmd/slopr/main.go`, construct:

```go
artifactStore := artifact.NewStore(db)
docTools := []docgen.Generator{
	docgen.TextGenerator{},
}
```

Pass `Artifacts: artifactStore`, `DocTools: docTools`, and `UsersDir: cfg.UsersDir` to `httpapi.New`.

- [ ] **Step 9: Run tests and verify pass**

Run: `cd backend && go test ./internal/httpapi`

Expected: pass.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/chat_types.go backend/internal/httpapi/message_stream_handlers.go backend/internal/httpapi/chat_test_helpers_test.go backend/internal/httpapi/message_stream_handlers_test.go backend/cmd/slopr/main.go
git commit -m "feat: execute built-in artifact tools"
```

## Task 5: Authenticated Artifact Download Endpoint

**Files:**
- Create: `backend/internal/httpapi/artifact_handlers.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing download tests**

Add to `backend/internal/httpapi/server_test.go`:

```go
func TestDownloadArtifactRequiresOwningUser(t *testing.T) {
	usersDir := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	artifacts := artifact.NewStore(db)
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(usersDir, testUser.ID, "files", "outputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usersDir, testUser.ID, "files", "outputs", "report.txt"), []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := artifacts.Create(context.Background(), artifact.CreateInput{
		UserID:          testUser.ID,
		ThreadID:        "thread_1",
		DisplayFilename: "report.txt",
		VolumeRelPath:   "files/outputs/report.txt",
		MIMEType:        "text/plain; charset=utf-8",
		SizeBytes:       6,
	})
	if err != nil {
		t.Fatal(err)
	}

	server := newAuthenticatedChatServer(t, Deps{Artifacts: artifacts, UsersDir: usersDir})
	req := authenticatedRequest(http.MethodGet, "/api/artifacts/"+created.ID+"/download", "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "report" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "report.txt") {
		t.Fatalf("Content-Disposition = %q", got)
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run: `cd backend && go test ./internal/httpapi -run TestDownloadArtifactRequiresOwningUser`

Expected: fail because route and handler do not exist.

- [ ] **Step 3: Add download handler**

Create `backend/internal/httpapi/artifact_handlers.go`:

```go
package httpapi

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

func (s *server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.artifacts == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), user.ID, r.PathValue("artifactID"))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "load artifact failed")
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	abs, err := artifact.ResolveExisting(s.usersDir, user.ID, found.VolumeRelPath)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "artifact path rejected")
		return
	}
	data, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		writeJSONError(w, http.StatusGone, "artifact file is missing")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "read artifact failed")
		return
	}
	w.Header().Set("Content-Type", found.MIMEType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Disposition", `attachment; filename="`+headerSafeFilename(found.DisplayFilename)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func headerSafeFilename(filename string) string {
	filename = strings.ReplaceAll(filename, "\r", "_")
	filename = strings.ReplaceAll(filename, "\n", "_")
	filename = strings.ReplaceAll(filename, `"`, "_")
	filename = strings.ReplaceAll(filename, `\`, "_")
	return filename
}
```

- [ ] **Step 4: Wire route**

In `backend/internal/httpapi/server.go`, add:

```go
mux.Handle("GET /api/artifacts/{artifactID}/download", s.requireAuth(http.HandlerFunc(s.handleDownloadArtifact)))
```

- [ ] **Step 5: Run tests and verify pass**

Run: `cd backend && go test ./internal/httpapi`

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/artifact_handlers.go backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: serve generated artifacts"
```

## Task 6: Frontend Artifact Cards

**Files:**
- Modify: `frontend/src/api.ts`
- Modify: `frontend/src/api.test.ts`
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Add failing API tests**

Append to `frontend/src/api.test.ts`:

```ts
test("streamMessage dispatches artifact events", async () => {
  const body = [
    'event: artifact',
    'data: {"id":"art_1","displayFilename":"notes.md","mimeType":"text/markdown; charset=utf-8","sizeBytes":7,"downloadUrl":"/api/artifacts/art_1/download"}',
    "",
    'event: done',
    'data: {}',
    "",
  ].join("\n");
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));
  const onArtifact = vi.fn();

  await streamMessage("t1", "make file", {
    onUserMessage: vi.fn(),
    onDelta: vi.fn(),
    onAssistantMessage: vi.fn(),
    onThread: vi.fn(),
    onArtifact,
  });

  expect(onArtifact).toHaveBeenCalledWith(expect.objectContaining({ displayFilename: "notes.md" }));
});

test("downloadArtifact fetches the artifact blob", async () => {
  const blob = new Blob(["hello"], { type: "text/plain" });
  const fetchMock = vi.fn().mockResolvedValue(new Response(blob));
  vi.stubGlobal("fetch", fetchMock);

  await expect(downloadArtifact("/api/artifacts/art_1/download")).resolves.toBeInstanceOf(Blob);
  expect(fetchMock).toHaveBeenCalledWith("/api/artifacts/art_1/download");
});
```

- [ ] **Step 2: Run API tests and verify failure**

Run: `cd frontend && npm run test -- --run src/api.test.ts`

Expected: fail because artifact types and helper do not exist.

- [ ] **Step 3: Add frontend artifact API types**

In `frontend/src/api.ts`, add:

```ts
export type Artifact = {
  id: string;
  displayFilename: string;
  mimeType: string;
  sizeBytes: number;
  projectId?: string;
  downloadUrl: string;
};
```

Add to `Message`:

```ts
artifacts?: Artifact[];
```

Add to `StreamHandlers`:

```ts
onArtifact?(artifact: Artifact): void;
```

Add helper:

```ts
export async function downloadArtifact(downloadUrl: string): Promise<Blob> {
  const response = await fetch(downloadUrl);
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to download artifact");
  }
  return response.blob();
}
```

Add `dispatchSSEEvent` case:

```ts
case "artifact":
  handlers.onArtifact?.(payload as Artifact);
  break;
```

- [ ] **Step 4: Add failing ChatShell tests**

Append to `frontend/src/App.test.tsx`:

```tsx
test("renders artifact card from streamed artifact event", async () => {
  vi.stubGlobal("fetch", chatThreadFetch({
    streamEvents: [
      { event: "user_message", data: { id: "m1", threadId: "t1", role: "user", content: "make file", createdAt: "2026-06-03T00:00:00Z" } },
      { event: "artifact", data: { id: "art_1", displayFilename: "notes.md", mimeType: "text/markdown; charset=utf-8", sizeBytes: 7, downloadUrl: "/api/artifacts/art_1/download" } },
      { event: "assistant_message", data: { id: "m2", threadId: "t1", role: "assistant", content: "Created notes.md.", artifacts: [{ id: "art_1", displayFilename: "notes.md", mimeType: "text/markdown; charset=utf-8", sizeBytes: 7, downloadUrl: "/api/artifacts/art_1/download" }], createdAt: "2026-06-03T00:00:01Z" } },
      { event: "done", data: {} },
    ],
  }));

  render(<App />);
  fireEvent.change(await screen.findByRole("textbox"), { target: { value: "make file" } });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));

  expect(await screen.findByText("notes.md")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download notes.md" })).toBeInTheDocument();
});
```

- [ ] **Step 5: Implement artifact cards**

In `frontend/src/ChatShell.tsx`, import `Artifact` and `downloadArtifact`.

Add local component:

```tsx
function ArtifactCard({ artifact }: { artifact: Artifact }) {
  const [error, setError] = useState<string | null>(null);
  async function handleDownload() {
    setError(null);
    try {
      const blob = await downloadArtifact(artifact.downloadUrl);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = artifact.displayFilename;
      link.click();
      URL.revokeObjectURL(url);
    } catch {
      setError("Download failed");
    }
  }
  return (
    <div className="mt-3 flex items-center gap-3 rounded-slopr border border-line bg-panel px-3 py-2 text-sm">
      <div className="flex h-9 w-9 items-center justify-center rounded bg-bg font-semibold uppercase text-muted">
        {artifact.displayFilename.split(".").pop()?.slice(0, 3) ?? "file"}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-ink">{artifact.displayFilename}</div>
        <div className="text-xs text-muted">{formatBytes(artifact.sizeBytes)}</div>
        {error ? <div className="text-xs text-danger">{error}</div> : null}
      </div>
      <button className="rounded border border-line px-3 py-1 text-ink hover:bg-bg" type="button" onClick={handleDownload} aria-label={`Download ${artifact.displayFilename}`}>
        Download
      </button>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
```

Render each message's `artifacts` after the assistant content and handle live `onArtifact` by appending the artifact to the current pending assistant message state.

- [ ] **Step 6: Run frontend tests and verify pass**

Run: `cd frontend && npm run test -- --run src/api.test.ts src/App.test.tsx`

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api.ts frontend/src/api.test.ts frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat(ui): render generated artifact cards"
```

## Task 7: XLSX and PDF Generators

**Files:**
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`
- Create: `backend/internal/docgen/xlsx.go`
- Create: `backend/internal/docgen/xlsx_test.go`
- Create: `backend/internal/docgen/pdf.go`
- Create: `backend/internal/docgen/pdf_test.go`
- Modify: `backend/cmd/slopr/main.go`

- [ ] **Step 1: Add dependencies**

Run: `cd backend && go get github.com/xuri/excelize/v2 github.com/signintech/gopdf`

Expected: `go.mod` and `go.sum` include the new libraries.

- [ ] **Step 2: Write XLSX smoke test**

Create `backend/internal/docgen/xlsx_test.go`:

```go
package docgen

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestXLSXGeneratorCreatesWorkbook(t *testing.T) {
	var out bytes.Buffer
	meta, err := XLSXGenerator{}.Generate(GenerateRequest{
		Filename: "sales.xlsx",
		Payload: map[string]any{
			"csvData": "Product,Sales\nA,10\nB,20",
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "xlsx" || out.Len() == 0 {
		t.Fatalf("meta=%#v len=%d", meta, out.Len())
	}
	book, err := excelize.OpenReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer book.Close()
	value, err := book.GetCellValue("Sheet1", "A2")
	if err != nil {
		t.Fatal(err)
	}
	if value != "A" {
		t.Fatalf("A2 = %q", value)
	}
}
```

- [ ] **Step 3: Write PDF smoke test**

Create `backend/internal/docgen/pdf_test.go`:

```go
package docgen

import (
	"bytes"
	"testing"
)

func TestPDFGeneratorCreatesPDF(t *testing.T) {
	var out bytes.Buffer
	meta, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "report.pdf",
		Payload: map[string]any{
			"content": "# Report\n\nHello from Slopr.",
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "pdf" {
		t.Fatalf("Extension = %q", meta.Extension)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF")) {
		t.Fatalf("PDF does not start with %%PDF, first bytes=%q", out.Bytes()[:4])
	}
}
```

- [ ] **Step 4: Run tests and verify failure**

Run: `cd backend && go test ./internal/docgen`

Expected: fail because generators do not exist.

- [ ] **Step 5: Implement XLSX generator**

Create `backend/internal/docgen/xlsx.go` using `excelize.NewFile`, `NewSheet`, `SetCellValue`,
`SetPanes`, and `Write`:

```go
package docgen

import (
	"encoding/csv"
	"errors"
	"io"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/xuri/excelize/v2"
)

const maxXLSXRows = 5000

type XLSXGenerator struct{}

func (g XLSXGenerator) ToolName() string { return "create_xlsx_file" }

func (g XLSXGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create an XLSX spreadsheet from CSV data.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"csvData":  map[string]any{"type": "string"},
			},
			"required":             []string{"filename", "csvData"},
			"additionalProperties": false,
		},
	}
}

func (g XLSXGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	raw, ok := req.Payload["csvData"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return GeneratedMeta{}, errors.New("csvData is required")
	}
	rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if err != nil {
		return GeneratedMeta{}, err
	}
	if len(rows) > maxXLSXRows {
		return GeneratedMeta{}, errors.New("too many rows")
	}
	book := excelize.NewFile()
	defer book.Close()
	for r, row := range rows {
		for c, value := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return GeneratedMeta{}, err
			}
			if err := book.SetCellValue("Sheet1", cell, value); err != nil {
				return GeneratedMeta{}, err
			}
		}
	}
	_ = book.SetPanes("Sheet1", &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	if err := book.Write(w); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "xlsx", MIMEType: artifact.MIMEType("xlsx")}, nil
}
```

- [ ] **Step 6: Implement PDF generator**

Create `backend/internal/docgen/pdf.go`:

```go
package docgen

import (
	"errors"
	"io"
	"strings"

	"github.com/signintech/gopdf"
	"github.com/trick77/slopr/internal/artifact"
)

type PDFGenerator struct{}

func (g PDFGenerator) ToolName() string { return "create_pdf_file" }

func (g PDFGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create a simple PDF from Markdown or plain text content.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string"},
			},
			"required":             []string{"filename", "content"},
			"additionalProperties": false,
		},
	}
}

func (g PDFGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	content, ok := req.Payload["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content is required")
	}
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4, Unit: gopdf.UnitMM})
	pdf.AddPage()
	pdf.SetXY(18, 20)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			_ = pdf.SetFont("Helvetica", "B", 18)
			_ = pdf.Cell(nil, strings.TrimPrefix(line, "# "))
		} else {
			_ = pdf.SetFont("Helvetica", "", 11)
			_ = pdf.Cell(nil, strings.TrimSpace(line))
		}
		pdf.Br(7)
	}
	if _, err := pdf.WriteTo(w); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "pdf", MIMEType: artifact.MIMEType("pdf")}, nil
}
```

- [ ] **Step 7: Register generators**

In `backend/cmd/slopr/main.go`, update `docTools`:

```go
docTools := []docgen.Generator{
	docgen.TextGenerator{},
	docgen.XLSXGenerator{},
	docgen.PDFGenerator{},
}
```

- [ ] **Step 8: Run tests and verify pass**

Run: `cd backend && go test ./internal/docgen ./internal/httpapi`

Expected: pass.

- [ ] **Step 9: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/docgen/xlsx.go backend/internal/docgen/xlsx_test.go backend/internal/docgen/pdf.go backend/internal/docgen/pdf_test.go backend/cmd/slopr/main.go
git commit -m "feat: generate pdf and xlsx artifacts"
```

## Task 8: DOCX and PPTX Generators

**Files:**
- Create: `backend/internal/docgen/ooxml.go`
- Create: `backend/internal/docgen/docx.go`
- Create: `backend/internal/docgen/docx_test.go`
- Create: `backend/internal/docgen/pptx.go`
- Create: `backend/internal/docgen/pptx_test.go`
- Modify: `backend/cmd/slopr/main.go`

- [ ] **Step 1: Write DOCX smoke test**

Create `backend/internal/docgen/docx_test.go`:

```go
package docgen

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestDOCXGeneratorCreatesWordPackage(t *testing.T) {
	var out bytes.Buffer
	meta, err := DOCXGenerator{}.Generate(GenerateRequest{
		Filename: "report.docx",
		Payload: map[string]any{
			"title":   "Report",
			"content": "# Report\n\nHello Slopr.",
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "docx" {
		t.Fatalf("Extension = %q", meta.Extension)
	}
	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	assertZipEntry(t, reader, "word/document.xml")
}
```

- [ ] **Step 2: Write PPTX smoke test**

Create `backend/internal/docgen/pptx_test.go`:

```go
package docgen

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestPPTXGeneratorCreatesPresentationPackage(t *testing.T) {
	var out bytes.Buffer
	meta, err := PPTXGenerator{}.Generate(GenerateRequest{
		Filename: "deck.pptx",
		Payload: map[string]any{
			"title": "Slopr Update",
			"slides": []any{
				map[string]any{"title": "Status", "bullets": []any{"Generated artifacts", "Download cards"}},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "pptx" {
		t.Fatalf("Extension = %q", meta.Extension)
	}
	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	assertZipEntry(t, reader, "ppt/presentation.xml")
	assertZipEntry(t, reader, "ppt/slides/slide1.xml")
}
```

Add shared test helper in one test file:

```go
func assertZipEntry(t *testing.T, reader *zip.Reader, name string) {
	t.Helper()
	for _, file := range reader.File {
		if file.Name == name {
			return
		}
	}
	t.Fatalf("missing zip entry %s", name)
}
```

- [ ] **Step 3: Run tests and verify failure**

Run: `cd backend && go test ./internal/docgen -run 'DOCX|PPTX'`

Expected: fail because generators do not exist.

- [ ] **Step 4: Add OOXML helper**

Create `backend/internal/docgen/ooxml.go`:

```go
package docgen

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
)

func writeZip(w io.Writer, files map[string]string) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		entry, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(entry, body); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func xmlText(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
```

- [ ] **Step 5: Implement DOCX generator**

Create `backend/internal/docgen/docx.go`:

```go
package docgen

import (
	"errors"
	"io"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

type DOCXGenerator struct{}

func (g DOCXGenerator) ToolName() string { return "create_docx_file" }

func (g DOCXGenerator) Schema() ToolSchema {
	return ToolSchema{Name: g.ToolName(), Description: "Create a DOCX document from Markdown or plain text content.", Parameters: map[string]any{"type": "object"}}
}

func (g DOCXGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	content, ok := req.Payload["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content is required")
	}
	body := docxBody(content)
	files := map[string]string{
		"word/document.xml":  body,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
	}
	files["[Content_Types].xml"] = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`
	files["_rels/.rels"] = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	if err := writeZip(w, files); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "docx", MIMEType: artifact.MIMEType("docx")}, nil
}

func docxBody(content string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line == "" {
			continue
		}
		b.WriteString(`<w:p><w:r><w:t>`)
		b.WriteString(xmlText(line))
		b.WriteString(`</w:t></w:r></w:p>`)
	}
	b.WriteString(`<w:sectPr/></w:body></w:document>`)
	return b.String()
}
```

- [ ] **Step 6: Implement PPTX generator**

Create `backend/internal/docgen/pptx.go`:

```go
package docgen

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

type PPTXGenerator struct{}

func (g PPTXGenerator) ToolName() string { return "create_pptx_presentation" }

func (g PPTXGenerator) Schema() ToolSchema {
	return ToolSchema{Name: g.ToolName(), Description: "Create a simple PPTX presentation from structured slide data.", Parameters: map[string]any{"type": "object"}}
}

func (g PPTXGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	title, _ := req.Payload["title"].(string)
	if strings.TrimSpace(title) == "" {
		title = "Presentation"
	}
	rawSlides, ok := req.Payload["slides"].([]any)
	if !ok || len(rawSlides) == 0 {
		return GeneratedMeta{}, errors.New("slides are required")
	}
	if len(rawSlides) > 30 {
		return GeneratedMeta{}, errors.New("too many slides")
	}
	files := pptxPackage(title, rawSlides)
	if err := writeZip(w, files); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "pptx", MIMEType: artifact.MIMEType("pptx")}, nil
}

func pptxPackage(title string, slides []any) map[string]string {
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/></Types>`,
		"_rels/.rels":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>`,
		"ppt/presentation.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:sldIdLst><p:sldId id="256" r:id="rId1" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`,
	}
	for i, raw := range slides {
		slideTitle := fmt.Sprintf("Slide %d", i+1)
		if m, ok := raw.(map[string]any); ok {
			if v, ok := m["title"].(string); ok && strings.TrimSpace(v) != "" {
				slideTitle = v
			}
		}
		files[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = pptxSlideXML(slideTitle)
	}
	return files
}

func pptxSlideXML(title string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:bodyPr/><a:p><a:r><a:t>` + xmlText(title) + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
}
```

- [ ] **Step 7: Register generators**

In `backend/cmd/slopr/main.go`, update `docTools`:

```go
docTools := []docgen.Generator{
	docgen.TextGenerator{},
	docgen.XLSXGenerator{},
	docgen.PDFGenerator{},
	docgen.DOCXGenerator{},
	docgen.PPTXGenerator{},
}
```

- [ ] **Step 8: Run tests and verify pass**

Run: `cd backend && go test ./internal/docgen ./internal/httpapi`

Expected: pass.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/docgen/ooxml.go backend/internal/docgen/docx.go backend/internal/docgen/docx_test.go backend/internal/docgen/pptx.go backend/internal/docgen/pptx_test.go backend/cmd/slopr/main.go
git commit -m "feat: generate docx and pptx artifacts"
```

## Task 9: Full Verification and Build Hygiene

**Files:**
- Modify docs only if implementation discoveries change the public behavior:
  - `README.md`
  - `.env.example`

- [ ] **Step 1: Run backend tests**

Run: `make test`

Expected: pass.

- [ ] **Step 2: Run frontend tests**

Run: `make fe-test`

Expected: pass.

- [ ] **Step 3: Build frontend**

Run: `make fe-build`

Expected: pass. If tracked `backend/web/dist/.gitkeep` or `backend/web/dist/index.html` changed, restore only those placeholders:

```bash
git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html
```

- [ ] **Step 4: Build binary**

Run: `make build`

Expected: pass and create `bin/slopr`.

- [ ] **Step 5: Smoke test dev server**

Run:

```bash
SLOPR_SESSION_SECRET=dev-secret SLOPR_AUTH_MODE=dev SLOPR_ADDR=127.0.0.1:18081 SLOPR_PUBLIC_URL=http://127.0.0.1:18081 SLOPR_DB_PATH=/private/tmp/slopr-docgen.db SLOPR_USERS_DIR=/private/tmp/slopr-docgen-users ./bin/slopr
```

In a second shell, run:

```bash
curl -i http://127.0.0.1:18081/api/health
```

Expected: `HTTP/1.1 200 OK`.

- [ ] **Step 6: Check worktree**

Run: `git status --short`

Expected: only intentional source changes are present. No built assets are left staged or unstaged.

- [ ] **Step 7: Commit docs updates if needed**

If README or env docs changed:

```bash
git add README.md .env.example
git commit -m "docs: document generated artifacts"
```

If no docs changed, skip this commit.
