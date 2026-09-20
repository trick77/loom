package documents

import (
	"io"
	"os"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/rag"
)

// VolumeOpener opens a document's bytes from the per-user volume, enforcing the
// artifact sandbox (reject .., absolute paths, symlink escape). It implements
// rag.FileOpener for the ingest pipeline.
type VolumeOpener struct {
	UsersDir string
}

// OpenDocument opens the file backing a document for reading.
func (v VolumeOpener) OpenDocument(d rag.Document) (io.ReadCloser, error) {
	abs, err := artifact.ResolveExisting(v.UsersDir, d.UserID, d.VolumeRelpath)
	if err != nil {
		return nil, err
	}
	return os.Open(abs) //nolint:gosec // path comes from artifact.ResolveExisting, which rejects absolute paths and .. and verifies containment under the user root after symlink resolution
}
