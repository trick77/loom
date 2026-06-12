package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UploadRequest describes a user upload to be written into the per-user volume.
// Unlike OutputRequest, uploads land in the knowledge folders (files/ or
// projects/<id>/) rather than the thread results folder (.../outputs/).
type UploadRequest struct {
	UsersDir        string
	UserID          string
	ProjectID       *string
	DisplayFilename string
	Extension       string
}

// CreateUploadFile exclusively creates a file for an upload inside the user's
// volume, applying the same sandbox guarantees as CreateOutputFile (reject ..,
// absolute paths, symlink escape) and collision-free naming. Global uploads go
// to files/; project uploads to projects/<projectID>/.
func CreateUploadFile(req UploadRequest) (OutputPath, *os.File, error) {
	if strings.TrimSpace(req.UsersDir) == "" {
		return OutputPath{}, nil, errors.New("users dir is required")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return OutputPath{}, nil, errors.New("user id is required")
	}
	extension := normalizeExtension(req.Extension)
	if extension == "" {
		return OutputPath{}, nil, errors.New("extension is required")
	}
	display, err := sanitizeDisplayFilename(req.DisplayFilename, extension)
	if err != nil {
		return OutputPath{}, nil, err
	}

	baseRel := "files"
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		projectID := *req.ProjectID
		// Reject a project id that would escape the volume; it is a path segment.
		if filepath.IsAbs(projectID) || strings.Contains(filepath.ToSlash(projectID), "..") || strings.ContainsAny(projectID, `/\`) {
			return OutputPath{}, nil, errors.New("invalid project id")
		}
		baseRel = filepath.Join("projects", projectID)
	}

	userRoot := filepath.Join(req.UsersDir, req.UserID)
	outputDir := filepath.Join(userRoot, baseRel)
	if err := ensureInside(userRoot, outputDir); err != nil {
		return OutputPath{}, nil, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return OutputPath{}, nil, fmt.Errorf("create upload directory: %w", err)
	}
	if err := ensureResolvedInside(userRoot, outputDir); err != nil {
		return OutputPath{}, nil, err
	}

	prepared := preparedOutput{baseRel: baseRel, extension: extension}
	ext := filepath.Ext(display)
	stem := strings.TrimSuffix(display, ext)
	candidate := display
	for i := 2; ; i++ {
		abs := filepath.Join(outputDir, candidate)
		file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
			continue
		}
		if err != nil {
			return OutputPath{}, nil, err
		}
		return prepared.path(candidate, abs), file, nil
	}
}
