package httpapi

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func volumeFiles(t *testing.T, usersDir string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(usersDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files
}

// A failed record insert used to leave the written file (and its thumbnail)
// on disk in two of the three copies of this sequence.
func TestPersistArtifactBytesRemovesFileAndThumbnailWhenTheRecordFails(t *testing.T) {
	usersDir := t.TempDir()
	s := &server{usersDir: usersDir, artifacts: fakeArtifactStore{createErr: errors.New("db down")}}

	_, err := s.persistArtifactBytes(context.Background(), testUser, chat.Thread{ID: "thr_1"}, artifactSpec{
		DisplayFilename: "chart.png",
		Extension:       "png",
		Data:            testPNG(t),
		Thumbnail:       true,
	})
	if err == nil {
		t.Fatal("persistArtifactBytes() error = nil, want the store failure")
	}
	if files := volumeFiles(t, usersDir); len(files) != 0 {
		t.Fatalf("files left behind after a failed persist: %v", files)
	}
}

func TestPersistArtifactBytesWritesTheFileAndReturnsTheRecord(t *testing.T) {
	usersDir := t.TempDir()
	var created artifact.CreateInput
	s := &server{usersDir: usersDir, artifacts: fakeArtifactStore{created: &created}}

	got, err := s.persistArtifactBytes(context.Background(), testUser, chat.Thread{ID: "thr_1"}, artifactSpec{
		DisplayFilename: "notes.txt",
		Extension:       "txt",
		Data:            []byte("hello"),
	})
	if err != nil {
		t.Fatalf("persistArtifactBytes() error = %v", err)
	}
	if created.SizeBytes != 5 || !strings.HasPrefix(created.MIMEType, "text/plain") || created.ThumbnailRelPath != "" {
		t.Fatalf("CreateInput = %+v, want 5 bytes of text/plain and no thumbnail", created)
	}
	data, err := os.ReadFile(filepath.Join(usersDir, testUser.ID, filepath.FromSlash(created.VolumeRelPath)))
	if err != nil || string(data) != "hello" {
		t.Fatalf("file on disk = %q (err %v), want hello", data, err)
	}
	if got.ID != "art_created" {
		t.Fatalf("returned artifact id = %q, want the store's", got.ID)
	}
}
