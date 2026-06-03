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
	prepared, err := prepareOutput(req)
	if err != nil {
		return OutputPath{}, err
	}
	finalName, finalAbs := collisionFreeName(prepared.outputDir, prepared.display)
	return prepared.path(finalName, finalAbs), nil
}

func CreateOutputFile(req OutputRequest) (OutputPath, *os.File, error) {
	prepared, err := prepareOutput(req)
	if err != nil {
		return OutputPath{}, nil, err
	}
	ext := filepath.Ext(prepared.display)
	stem := strings.TrimSuffix(prepared.display, ext)
	candidate := prepared.display
	for i := 2; ; i++ {
		abs := filepath.Join(prepared.outputDir, candidate)
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

type preparedOutput struct {
	userRoot  string
	baseRel   string
	outputDir string
	display   string
	extension string
}

func prepareOutput(req OutputRequest) (preparedOutput, error) {
	if strings.TrimSpace(req.UsersDir) == "" {
		return preparedOutput{}, errors.New("users dir is required")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return preparedOutput{}, errors.New("user id is required")
	}
	extension := normalizeExtension(req.Extension)
	if extension == "" {
		return preparedOutput{}, errors.New("extension is required")
	}
	display, err := sanitizeDisplayFilename(req.DisplayFilename, extension)
	if err != nil {
		return preparedOutput{}, err
	}
	baseRel := filepath.Join("files", "outputs")
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		baseRel = filepath.Join("projects", *req.ProjectID, "outputs")
	}
	userRoot := filepath.Join(req.UsersDir, req.UserID)
	outputDir := filepath.Join(userRoot, baseRel)
	if err := ensureInside(userRoot, outputDir); err != nil {
		return preparedOutput{}, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return preparedOutput{}, fmt.Errorf("create output directory: %w", err)
	}
	if err := ensureResolvedInside(userRoot, outputDir); err != nil {
		return preparedOutput{}, err
	}
	return preparedOutput{
		userRoot:  userRoot,
		baseRel:   baseRel,
		outputDir: outputDir,
		display:   display,
		extension: extension,
	}, nil
}

func (p preparedOutput) path(filename, abs string) OutputPath {
	rel := filepath.ToSlash(filepath.Join(p.baseRel, filename))
	return OutputPath{
		AbsPath:         abs,
		VolumeRelPath:   rel,
		DisplayFilename: filename,
		MIMEType:        MIMEType(p.extension),
	}
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
	if err := ensureResolvedInside(userRoot, resolvedParent); err != nil {
		return "", err
	}
	return abs, nil
}

func ensureResolvedInside(root, path string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve user root: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve artifact path: %w", err)
	}
	if err := ensureInside(resolvedRoot, resolvedPath); err != nil {
		return err
	}
	return nil
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
