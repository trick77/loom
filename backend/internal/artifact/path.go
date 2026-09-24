package artifact

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var safeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

// CreateOutputFile reserves a unique filename and opens a new file for writing, handling filename collisions.
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
		file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // abs is filepath.Join(outputDir, sanitized-basename); outputDir was checked with ensureInside/ensureResolvedInside and the name passed sanitizeDisplayFilename, so it cannot escape the user root
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
		if err := validateProjectSegment(*req.ProjectID); err != nil {
			return preparedOutput{}, err
		}
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

// ResolveExisting validates and resolves the absolute path for a user-visible
// artifact, rejecting traversal, the reserved .loom subtree and symlink escapes.
func ResolveExisting(usersDir, userID, volumeRelPath string) (string, error) {
	clean, err := cleanVolumePath(volumeRelPath)
	if err != nil {
		return "", errors.New("invalid artifact path")
	}
	if clean == reservedDir || strings.HasPrefix(clean, reservedDir+"/") {
		return "", errors.New("reserved artifact path")
	}
	return resolveInside(filepath.Join(usersDir, userID), clean)
}

// reservedDir is the per-user subtree loom keeps for itself (thumbnails); user
// artifact paths may never point into it.
const reservedDir = ".loom"

// cleanVolumePath normalizes a volume-relative path to slash form and rejects
// anything that is not a plain relative path into the volume: absolute paths,
// an empty or "." path, and any ".." segment (a ".." inside a name, as in
// a..b.pdf, is fine).
func cleanVolumePath(rel string) (string, error) {
	slash := filepath.ToSlash(rel)
	if filepath.IsAbs(rel) || strings.HasPrefix(slash, "/") {
		return "", errors.New("absolute path")
	}
	clean := path.Clean(slash)
	if clean == "." || clean == "" {
		return "", errors.New("empty path")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".." {
			return "", errors.New("path traversal")
		}
	}
	return clean, nil
}

// resolveInside maps a cleaned volume-relative path to its absolute location
// under userRoot and proves the location stays inside the root once symlinks
// are followed. The deepest existing prefix of the path is resolved (so a
// not-yet-written file still validates), and a final component that exists as
// a symlink is rejected outright: loom only ever writes regular files, so a
// symlink there is not something to follow.
func resolveInside(userRoot, clean string) (string, error) {
	abs := filepath.Join(userRoot, filepath.FromSlash(clean))
	if err := ensureInside(userRoot, abs); err != nil {
		return "", err
	}
	existing := abs
	for {
		info, err := os.Lstat(existing)
		if err == nil {
			if existing == abs && info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("artifact path is a symlink")
			}
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	// The root is its own deepest prefix for a path whose first component does
	// not exist yet; the root itself is trusted, only what hangs below it is
	// checked once symlinks are followed.
	if filepath.Clean(existing) != filepath.Clean(userRoot) {
		if err := ensureResolvedInside(userRoot, existing); err != nil {
			return "", err
		}
	}
	return abs, nil
}

// validateProjectSegment rejects a project id that would not stay a single path
// segment under the user's volume.
func validateProjectSegment(projectID string) error {
	if projectID == "" || projectID == "." || projectID == ".." || filepath.IsAbs(projectID) || strings.ContainsAny(projectID, `/\`) {
		return errors.New("invalid project id")
	}
	return nil
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
	if filepath.IsAbs(input) || strings.Contains(slashInput, "../") || strings.HasPrefix(slashInput, ".loom/") {
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

// SanitizeDisplayName cleans a user-supplied artifact display name to the same
// standard the upload path enforces: it strips the directory portion, removes
// null bytes and filesystem-unsafe characters, collapses surrounding spaces/dots,
// rejects path traversal, and caps the length. Unlike sanitizeDisplayFilename it
// does not force a particular extension — the rename handler locks the original
// extension separately. Returns an error when nothing usable remains.
func SanitizeDisplayName(input string) (string, error) {
	slashInput := filepath.ToSlash(input)
	if filepath.IsAbs(input) || strings.Contains(slashInput, "../") || strings.HasPrefix(slashInput, ".loom/") {
		return "", errors.New("invalid filename")
	}
	name := strings.TrimSpace(filepath.Base(input))
	name = strings.ReplaceAll(name, "\x00", "")
	name = safeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, " .")
	if name == "" {
		return "", errors.New("invalid filename")
	}
	if len(name) > MaxDisplayFilenameLength {
		ext := filepath.Ext(name)
		if len(ext) < MaxDisplayFilenameLength {
			name = strings.TrimSuffix(name, ext)[:MaxDisplayFilenameLength-len(ext)] + ext
		} else {
			name = name[:MaxDisplayFilenameLength]
		}
	}
	return name, nil
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
