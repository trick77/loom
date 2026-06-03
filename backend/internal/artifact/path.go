package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var safeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func ResolveOutputPath(req OutputRequest) (OutputPath, error) {
	if strings.TrimSpace(req.UsersDir) == "" {
		return OutputPath{}, errors.New("users dir is required")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return OutputPath{}, errors.New("user id is required")
	}
	extension := normalizeExtension(req.Extension)
	if extension == "" {
		return OutputPath{}, errors.New("extension is required")
	}
	display, err := sanitizeDisplayFilename(req.DisplayFilename, extension)
	if err != nil {
		return OutputPath{}, err
	}

	baseRel := filepath.Join("files", "outputs")
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		baseRel = filepath.Join("projects", *req.ProjectID, "outputs")
	}

	userRoot := filepath.Join(req.UsersDir, req.UserID)
	outputDir := filepath.Join(userRoot, baseRel)
	if err := ensureInside(userRoot, outputDir); err != nil {
		return OutputPath{}, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return OutputPath{}, fmt.Errorf("create output directory: %w", err)
	}
	finalName, finalAbs := collisionFreeName(outputDir, display)
	rel := filepath.ToSlash(filepath.Join(baseRel, finalName))
	return OutputPath{
		AbsPath:         finalAbs,
		VolumeRelPath:   rel,
		DisplayFilename: finalName,
		MIMEType:        MIMEType(extension),
	}, nil
}

func ResolveExisting(usersDir, userID, volumeRelPath string) (string, error) {
	if filepath.IsAbs(volumeRelPath) || strings.Contains(volumeRelPath, "..") {
		return "", errors.New("invalid artifact path")
	}
	if strings.HasPrefix(filepath.ToSlash(volumeRelPath), ".spark/") {
		return "", errors.New("reserved artifact path")
	}
	userRoot := filepath.Join(usersDir, userID)
	abs := filepath.Join(userRoot, filepath.FromSlash(volumeRelPath))
	if err := ensureInside(userRoot, abs); err != nil {
		return "", err
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("resolve artifact parent: %w", err)
	}
	if err := ensureInside(userRoot, resolvedParent); err != nil {
		return "", err
	}
	return abs, nil
}

func sanitizeDisplayFilename(input, extension string) (string, error) {
	slashInput := filepath.ToSlash(input)
	if filepath.IsAbs(input) || strings.Contains(slashInput, "../") || strings.HasPrefix(slashInput, ".spark/") {
		return "", errors.New("invalid filename")
	}
	name := strings.TrimSpace(filepath.Base(input))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "artifact." + extension
	}
	name = strings.ReplaceAll(name, "\x00", "")
	name = safeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, " .")
	if name == "" {
		name = "artifact." + extension
	}
	if !strings.EqualFold(filepath.Ext(name), "."+extension) {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + "." + extension
	}
	if len(name) > MaxDisplayFilenameLength {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		name = stem[:MaxDisplayFilenameLength-len(ext)] + ext
	}
	return name, nil
}

func collisionFreeName(dir, name string) (string, string) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	candidate := name
	for i := 2; ; i++ {
		abs := filepath.Join(dir, candidate)
		if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			return candidate, abs
		}
		candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
	}
}

func ensureInside(root, path string) error {
	rootClean, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathClean, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootClean, pathClean)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path escapes user root")
	}
	return nil
}

func normalizeExtension(extension string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(extension), "."))
}
