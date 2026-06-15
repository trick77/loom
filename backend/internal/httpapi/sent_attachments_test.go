package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/trick77/lume/internal/artifact"
	"github.com/trick77/lume/internal/chat"
	"github.com/trick77/lume/internal/rag"
)

func TestResolveSentAttachments_resolvesImageAndScopedDocument(t *testing.T) {
	s := &server{
		artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: "u1", DisplayFilename: "photo.png", MIMEType: "image/png", SizeBytes: 1234, DownloadURL: "/api/artifacts/art_1/download"},
		}},
		documents: &inlineStub{docs: map[string]rag.Document{
			"d1": {ID: "d1", Filename: "notes.md", MIME: "text/markdown", SizeBytes: 99, ThreadID: strPtr("t1")},
		}},
	}
	thread := chat.Thread{ID: "t1"}

	raw := s.resolveSentAttachments(context.Background(), "u1", thread, []string{"art_1"}, []string{"d1"})

	var got []chat.MessageAttachment
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d attachments, want 2: %s", len(got), raw)
	}
	if got[0].Kind != chat.AttachmentKindImage || got[0].ArtifactID != "art_1" || got[0].DownloadURL != "/api/artifacts/art_1/download" {
		t.Fatalf("image attachment = %+v", got[0])
	}
	if got[0].Filename != "photo.png" || got[0].MIMEType != "image/png" || got[0].SizeBytes != 1234 {
		t.Fatalf("image attachment metadata = %+v", got[0])
	}
	if got[1].Kind != chat.AttachmentKindDocument || got[1].DocumentID != "d1" || got[1].Filename != "notes.md" {
		t.Fatalf("document attachment = %+v", got[1])
	}
	if got[1].DownloadURL != "" {
		t.Fatalf("document attachment should have no download url yet, got %q", got[1].DownloadURL)
	}
}

func TestResolveSentAttachments_skipsForeignUserArtifact(t *testing.T) {
	s := &server{
		artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: "someone_else", DisplayFilename: "secret.png", MIMEType: "image/png"},
		}},
	}

	raw := s.resolveSentAttachments(context.Background(), "u1", chat.Thread{ID: "t1"}, []string{"art_1"}, nil)

	if string(raw) != "[]" {
		t.Fatalf("foreign artifact must not attach, got %s", raw)
	}
}

func TestResolveSentAttachments_skipsOutOfScopeDocument(t *testing.T) {
	s := &server{
		documents: &inlineStub{docs: map[string]rag.Document{
			"d1": {ID: "d1", Filename: "other.md", ThreadID: strPtr("other-thread")},
		}},
	}

	raw := s.resolveSentAttachments(context.Background(), "u1", chat.Thread{ID: "t1"}, nil, []string{"d1"})

	if string(raw) != "[]" {
		t.Fatalf("out-of-scope document must not attach, got %s", raw)
	}
}

func TestResolveSentAttachments_emptyWhenNoIDs(t *testing.T) {
	s := &server{}
	raw := s.resolveSentAttachments(context.Background(), "u1", chat.Thread{ID: "t1"}, nil, nil)
	if string(raw) != "[]" {
		t.Fatalf("want [], got %s", raw)
	}
}
