package artifact

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountThreadUploadsCountsOnlyOwnPrivateUploads(t *testing.T) {
	project := "p1"
	items := []Artifact{
		{UserID: "u", Source: SourceUserUploaded},
		{UserID: "u", Source: SourceUserUploaded},
		{UserID: "u", Source: SourceUserUploaded, ProjectID: &project},
		{UserID: "u", Source: "assistant_generated"},
		{UserID: "other", Source: SourceUserUploaded},
	}
	if got := CountThreadUploads(items, "u"); got != 2 {
		t.Fatalf("CountThreadUploads() = %d, want 2", got)
	}
}

type fakeUploadStore struct {
	thread    []Artifact
	listErr   error
	createErr error
	created   []CreateInput
}

func (f *fakeUploadStore) ListForThread(context.Context, string, string) ([]Artifact, error) {
	return f.thread, f.listErr
}

func (f *fakeUploadStore) Create(_ context.Context, in CreateInput) (Artifact, error) {
	if f.createErr != nil {
		return Artifact{}, f.createErr
	}
	f.created = append(f.created, in)
	return Artifact{ID: "art_1", UserID: in.UserID, VolumeRelPath: in.VolumeRelPath, Source: in.Source}, nil
}

func uploadInput(usersDir string, content []byte) StoreUploadInput {
	return StoreUploadInput{
		UsersDir:        usersDir,
		UserID:          "u",
		ThreadID:        "t1",
		DisplayFilename: "a.png",
		Extension:       "png",
		MIMEType:        "image/png",
		Reader:          bytes.NewReader(content),
		ThreadLimit:     2,
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// userFiles lists every file left under the user's volume.
func userFiles(t *testing.T, usersDir string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(filepath.Join(usersDir, "u"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func TestStoreUploadWritesAndRecords(t *testing.T) {
	usersDir := t.TempDir()
	store := &fakeUploadStore{}
	in := uploadInput(usersDir, pngBytes(t))
	in.Thumbnail = true
	stored, err := StoreUpload(context.Background(), store, in)
	if err != nil {
		t.Fatalf("StoreUpload() error = %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("Create calls = %d, want 1", len(store.created))
	}
	rec := store.created[0]
	if rec.Source != SourceUserUploaded || rec.ThreadID != "t1" || rec.SizeBytes != stored.Size || rec.ThumbnailRelPath == "" {
		t.Fatalf("recorded %+v, want user upload in t1 with size %d and a thumbnail", rec, stored.Size)
	}
	if _, err := os.Stat(stored.Path.AbsPath); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
}

func TestStoreUploadRefusesAtThreadLimit(t *testing.T) {
	usersDir := t.TempDir()
	store := &fakeUploadStore{thread: []Artifact{
		{UserID: "u", Source: SourceUserUploaded},
		{UserID: "u", Source: SourceUserUploaded},
	}}
	if _, err := StoreUpload(context.Background(), store, uploadInput(usersDir, []byte("x"))); !errors.Is(err, ErrThreadUploadLimit) {
		t.Fatalf("StoreUpload() error = %v, want ErrThreadUploadLimit", err)
	}
	// A project upload is not thread-private: the limit does not apply.
	in := uploadInput(usersDir, []byte("x"))
	project := "p1"
	in.ProjectID = &project
	if _, err := StoreUpload(context.Background(), store, in); err != nil {
		t.Fatalf("StoreUpload(project) error = %v", err)
	}
}

func TestStoreUploadFailureStages(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name  string
		store *fakeUploadStore
		edit  func(*StoreUploadInput)
		stage UploadStage
	}{
		{"count", &fakeUploadStore{listErr: boom}, func(*StoreUploadInput) {}, UploadStageCount},
		{"file", &fakeUploadStore{}, func(in *StoreUploadInput) { in.DisplayFilename = "../../x.png"; in.Extension = "" }, UploadStageFile},
		{"write", &fakeUploadStore{}, func(in *StoreUploadInput) { in.Reader = errReader{boom} }, UploadStageWrite},
		{"record", &fakeUploadStore{createErr: boom}, func(in *StoreUploadInput) { in.Thumbnail = true }, UploadStageRecord},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usersDir := t.TempDir()
			in := uploadInput(usersDir, pngBytes(t))
			tc.edit(&in)
			_, err := StoreUpload(context.Background(), tc.store, in)
			var uploadErr *UploadError
			if !errors.As(err, &uploadErr) || uploadErr.Stage != tc.stage {
				t.Fatalf("StoreUpload() error = %v, want stage %q", err, tc.stage)
			}
			if !strings.HasPrefix(err.Error(), string(tc.stage)+": ") {
				t.Fatalf("error text %q does not name stage %q", err, tc.stage)
			}
			if left := userFiles(t, usersDir); len(left) != 0 {
				t.Fatalf("files left behind: %v", left)
			}
		})
	}
}

func TestStoreUploadRefusesOversizedContent(t *testing.T) {
	usersDir := t.TempDir()
	in := uploadInput(usersDir, nil)
	in.Reader = bytes.NewReader(make([]byte, MaxArtifactSizeBytes+1))
	if _, err := StoreUpload(context.Background(), &fakeUploadStore{}, in); !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("StoreUpload() error = %v, want ErrUploadTooLarge", err)
	}
	if left := userFiles(t, usersDir); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
