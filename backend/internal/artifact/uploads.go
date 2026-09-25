package artifact

// SourceUserUploaded marks an artifact the user uploaded, as opposed to one
// the assistant generated.
const SourceUserUploaded = "user_uploaded"

// CountThreadUploads counts the user's own thread-private uploads among a
// thread's artifacts: the number the per-chat attachment limit applies to.
// Project-scoped uploads and generated artifacts do not count.
func CountThreadUploads(items []Artifact, userID string) int {
	count := 0
	for _, item := range items {
		if item.UserID == userID && item.ProjectID == nil && item.Source == SourceUserUploaded {
			count++
		}
	}
	return count
}
