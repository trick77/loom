package artifact

import "testing"

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
