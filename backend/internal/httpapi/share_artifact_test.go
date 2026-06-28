package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
)

// TestPublicShareArtifact_gatedByActiveShareAndAllowlist is the security contract
// for serving a shared thread's generated artifacts to anonymous viewers:
//   - served only while the share is active AND the id is in its snapshot allowlist
//   - once the share is disabled, BOTH the download and the thumbnail 404 — a known
//     direct URL stops working immediately
//   - a known artifact id that is not in the share's allowlist is never served
//   - an id from one share cannot be fetched through another share's URL
func TestPublicShareArtifact_gatedByActiveShareAndAllowlist(t *testing.T) {
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relPath := "files/outputs/art_1.png"
	absFile := filepath.Join(usersDir, "user_1", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absFile, pngFileBytes(t, 64, 64), 0o644); err != nil {
		t.Fatal(err)
	}

	artifacts := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", SizeBytes: 64},
		// art_secret exists and is owned by the same user but is NOT in any share.
		{ID: "art_secret", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "s.png", MIMEType: "image/png", SizeBytes: 64},
	}}
	threads := &fakeThreadStore{shares: map[string]chat.Share{
		"t1": {
			ID: "s1", ShareID: "SHARE_A", ThreadID: "t1", UserID: "user_1",
			Shared: true, ArtifactIDs: []string{"art_1"},
		},
	}}
	// Anonymous server: no Auth at all.
	srv := New(Deps{Thread: threads, Artifacts: artifacts, UsersDir: usersDir})

	get := func(path string) int {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}

	// While shared: the allowlisted artifact is served.
	if code := get("/api/shares/SHARE_A/artifacts/art_1/download"); code != http.StatusOK {
		t.Fatalf("download while shared = %d, want 200", code)
	}
	if code := get("/api/shares/SHARE_A/artifacts/art_1/thumbnail"); code != http.StatusOK {
		t.Fatalf("thumbnail while shared = %d, want 200", code)
	}

	// A known id that is NOT in the allowlist is never served, even while shared.
	if code := get("/api/shares/SHARE_A/artifacts/art_secret/download"); code != http.StatusNotFound {
		t.Fatalf("download of non-allowlisted id = %d, want 404", code)
	}

	// An id from share A cannot be fetched through a different (nonexistent) share.
	if code := get("/api/shares/SHARE_B/artifacts/art_1/download"); code != http.StatusNotFound {
		t.Fatalf("cross-share download = %d, want 404", code)
	}

	// Disable the share: the direct URLs stop working immediately — download AND thumbnail.
	threads.shares["t1"] = chat.Share{
		ID: "s1", ShareID: "SHARE_A", ThreadID: "t1", UserID: "user_1",
		Shared: false, ArtifactIDs: []string{"art_1"},
	}
	if code := get("/api/shares/SHARE_A/artifacts/art_1/download"); code != http.StatusNotFound {
		t.Fatalf("download after unshare = %d, want 404", code)
	}
	if code := get("/api/shares/SHARE_A/artifacts/art_1/thumbnail"); code != http.StatusNotFound {
		t.Fatalf("thumbnail after unshare = %d, want 404", code)
	}
}
