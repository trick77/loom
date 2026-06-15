package documents

import (
	"io"
	"os"

	"github.com/trick77/lume/internal/artifact"
	"github.com/trick77/lume/internal/rag"
)

// VolumeOpener opens a document's bytes from the per-user volume, enforcing the
// artifact sandbox (reject .., absolute paths, symlink escape). It implements
// rag.FileOpener for the ingest pipeline.
type VolumeOpener struct {
	UsersDir string
}

func (v VolumeOpener) OpenDocument(d rag.Document) (io.ReadCloser, error) {
	abs, err := artifact.ResolveExisting(v.UsersDir, d.UserID, d.VolumeRelpath)
	if err != nil {
		return nil, err
	}
	return os.Open(abs)
}
