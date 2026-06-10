package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/slopr/internal/artifact"
)

func TestListArtifactsReturnsCurrentUsersArtifacts(t *testing.T) {
	server := newAuthenticatedChatServer(t, Deps{
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{
			{
				ID:              "art_1",
				UserID:          "user_1",
				ThreadID:        "thread_1",
				DisplayFilename: "robot.png",
				MIMEType:        "image/png",
				SizeBytes:       842000,
				Source:          "assistant_generated",
				CreatedAt:       time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC),
				DownloadURL:     "/api/artifacts/art_1/download",
			},
			{
				ID:              "art_2",
				UserID:          "user_2",
				ThreadID:        "thread_2",
				DisplayFilename: "other.png",
				MIMEType:        "image/png",
				SizeBytes:       10,
				Source:          "uploaded",
				CreatedAt:       time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC),
				DownloadURL:     "/api/artifacts/art_2/download",
			},
		}},
	})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts?type=images&sort=modified&order=desc&search=robot", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var got []artifactListItemResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(response) = %d, want 1: %#v", len(got), got)
	}
	if got[0].ID != "art_1" || got[0].DisplayFilename != "robot.png" || got[0].ModifiedAt.IsZero() {
		t.Fatalf("response item = %#v", got[0])
	}
}
