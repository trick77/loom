package httpapi

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/trick77/lume/internal/artifact"
	"github.com/trick77/lume/internal/auth"
	"github.com/trick77/lume/internal/chat"
	"github.com/trick77/lume/internal/sse"
)

// Code/XML blocks below these bounds are treated as illustrative snippets and
// left inline only; larger ones are also offered as a downloadable artifact.
const (
	codeArtifactMinLines = 50
	codeArtifactMinChars = 2000
)

// codeBlockLanguageExtensions maps a fenced-code language tag to the file
// extension used for an auto-generated download. Only real programming
// languages plus xml qualify — prose, markdown, and structured-data fences are
// intentionally excluded so summaries and short data never spawn a file.
var codeBlockLanguageExtensions = map[string]string{
	"python": "py", "py": "py",
	"javascript": "js", "js": "js", "node": "js",
	"typescript": "ts", "ts": "ts",
	"jsx": "jsx", "tsx": "tsx",
	"go": "go", "golang": "go",
	"java":   "java",
	"kotlin": "kt", "kt": "kt",
	"c":      "c",
	"cpp":    "cpp", "c++": "cpp",
	"csharp": "cs", "cs": "cs",
	"rust":   "rs", "rs": "rs",
	"ruby":   "rb", "rb": "rb",
	"php":    "php",
	"swift":  "swift",
	"scala":  "scala",
	"sh":     "sh", "bash": "sh", "shell": "sh", "zsh": "sh",
	"sql":    "sql",
	"r":      "r",
	"lua":    "lua",
	"perl":   "pl", "pl": "pl",
	"dart":   "dart",
	"groovy": "groovy",
	"xml":    "xml",
}

// fencedCodeBlock is one ```lang … ``` (or ~~~) span extracted from the answer.
type fencedCodeBlock struct {
	language string
	body     string
	heading  string // nearest preceding heading/bold line, for the filename
}

var (
	codeFenceOpenRE  = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*([^`\\s]*)")
	headingLineRE    = regexp.MustCompile(`^ {0,3}#{1,6}\s+(.+?)\s*$`)
	boldLineRE       = regexp.MustCompile(`^\s*\*\*(.+?)\*\*\s*:?\s*$`)
	slugStripRE      = regexp.MustCompile(`[^a-z0-9]+`)
	slugTrimDashesRE = regexp.MustCompile(`^-+|-+$`)
)

// extractCodeArtifacts scans a finished assistant answer for large code/XML
// fences and turns each into a persisted, downloadable artifact, emitting an
// "artifact" SSE event for each. It returns the newly created artifacts so the
// caller can append them to the message before persistence. The work is purely
// deterministic and code-driven — the model is never asked to decide.
//
// When the model already produced an explicit (non-image) file this turn, the
// user asked for a save directly and we suppress auto-extraction so the same
// code is not duplicated as a second download.
func (s *server) extractCodeArtifacts(ctx context.Context, stream *sse.Writer, user auth.User, thread chat.Thread, content string, existing []artifactResponse) []artifactResponse {
	for _, a := range existing {
		if !strings.HasPrefix(a.MIMEType, "image/") {
			return nil
		}
	}

	var created []artifactResponse
	for _, q := range qualifyingCodeBlocks(content) {
		response, err := s.createCodeArtifact(ctx, user, thread, q.block, q.extension, q.filename)
		if err != nil {
			slog.Warn("auto code artifact failed", "thread_id", thread.ID, "filename", q.filename, "error", err)
			continue
		}
		_ = sendSSEJSON(stream, "artifact", response)
		created = append(created, response)
	}
	return created
}

// qualifiedBlock is a code block that passed the language and size gates, with
// its resolved download extension and filename.
type qualifiedBlock struct {
	block     fencedCodeBlock
	extension string
	filename  string
}

// qualifyingCodeBlocks is the pure, deterministic selection: which fenced blocks
// of a finished answer should become downloadable files, and under what name. It
// holds no I/O so it is exercised directly by tests.
func qualifyingCodeBlocks(content string) []qualifiedBlock {
	blocks := parseCodeBlocks(content)
	var out []qualifiedBlock
	seenBodies := make(map[string]bool)
	index := 0
	for _, block := range blocks {
		ext, ok := codeBlockLanguageExtensions[block.language]
		if !ok {
			// Blocks without a recognized language tag (including untagged fences)
			// are intentionally skipped: there is no reliable extension to give
			// them, and defaulting to .txt would turn plain output, tables, and
			// logs into the unwanted downloads this feature exists to avoid.
			continue
		}
		if !isDownloadWorthyCode(block.body) {
			continue
		}
		key := block.language + "\x00" + block.body
		if seenBodies[key] {
			continue
		}
		seenBodies[key] = true
		index++
		out = append(out, qualifiedBlock{
			block:     block,
			extension: ext,
			filename:  codeArtifactFilename(block, ext, index),
		})
	}
	return out
}

// isDownloadWorthyCode reports whether a code body is substantial enough to be
// a file rather than an inline snippet.
func isDownloadWorthyCode(body string) bool {
	if len(body) >= codeArtifactMinChars {
		return true
	}
	return strings.Count(body, "\n")+1 >= codeArtifactMinLines
}

func (s *server) createCodeArtifact(ctx context.Context, user auth.User, thread chat.Thread, block fencedCodeBlock, ext, filename string) (artifactResponse, error) {
	out, file, err := artifact.CreateOutputFile(artifact.OutputRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: filename,
		Extension:       ext,
	})
	if err != nil {
		return artifactResponse{}, err
	}
	payload := []byte(block.body)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		_ = os.Remove(out.AbsPath)
		return artifactResponse{}, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(out.AbsPath)
		return artifactResponse{}, err
	}
	record, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: out.DisplayFilename,
		VolumeRelPath:   out.VolumeRelPath,
		MIMEType:        out.MIMEType,
		SizeBytes:       int64(len(payload)),
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		return artifactResponse{}, err
	}
	return artifactResponse{
		ID:              record.ID,
		DisplayFilename: record.DisplayFilename,
		MIMEType:        record.MIMEType,
		SizeBytes:       record.SizeBytes,
		ProjectID:       record.ProjectID,
		DownloadURL:     record.DownloadURL,
	}, nil
}

// codeArtifactFilename derives a meaningful name from the nearest heading above
// the block, falling back to code-N.<ext> when there is none.
func codeArtifactFilename(block fencedCodeBlock, ext string, index int) string {
	if slug := slugify(block.heading); slug != "" {
		return slug + "." + ext
	}
	return "code-" + strconv.Itoa(index) + "." + ext
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugStripRE.ReplaceAllString(s, "-")
	s = slugTrimDashesRE.ReplaceAllString(s, "")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s
}

// parseCodeBlocks extracts fenced code blocks (``` or ~~~) from markdown,
// recording each block's language tag and the nearest preceding heading/bold
// line for naming. Closing fences must use the same character and be at least
// as long as the opener, per CommonMark.
func parseCodeBlocks(content string) []fencedCodeBlock {
	lines := strings.Split(content, "\n")
	var blocks []fencedCodeBlock
	lastHeading := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		open := codeFenceOpenRE.FindStringSubmatch(line)
		if open == nil {
			if h := headingText(line); h != "" {
				lastHeading = h
			} else if strings.TrimSpace(line) != "" {
				// Intervening prose detaches a pending heading so only a heading
				// directly above the fence (blank lines aside) names the file.
				lastHeading = ""
			}
			continue
		}
		fence := open[1]
		language := strings.ToLower(strings.TrimSpace(open[2]))
		var bodyLines []string
		i++
		for ; i < len(lines); i++ {
			if isClosingFence(lines[i], fence) {
				break
			}
			bodyLines = append(bodyLines, lines[i])
		}
		blocks = append(blocks, fencedCodeBlock{
			language: language,
			body:     strings.Join(bodyLines, "\n"),
			heading:  lastHeading,
		})
		lastHeading = ""
	}
	return blocks
}

// headingText returns the text of a markdown heading or bold-only line, or "".
func headingText(line string) string {
	if m := headingLineRE.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	if m := boldLineRE.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

func isClosingFence(line, fence string) bool {
	trimmed := strings.TrimRight(strings.TrimLeft(line, " "), " \t")
	if trimmed == "" {
		return false
	}
	marker := fence[:1]
	count := 0
	for count < len(trimmed) && string(trimmed[count]) == marker {
		count++
	}
	return count >= len(fence) && count == len(trimmed)
}
