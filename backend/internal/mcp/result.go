package mcp

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/trick77/loom/internal/artifact"
)

// maxStructuredSources caps how many sources one tool result may contribute, so
// a misbehaving server cannot flood the sources sidebar.
const maxStructuredSources = 20

// Source is one citable item a tool result carries in
// structuredContent.sources: the convention loom reads so any MCP server can
// feed the sources sidebar and the model's [n] markers. URL is required; the
// rest is display detail; Image is a preview picture (a video thumbnail).
type Source struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Label   string `json:"label"`
	Snippet string `json:"snippet"`
	Image   string `json:"image"`
}

// CallResult is a tool call's model-facing text plus the sources it cited,
// the files it handed to run_python and the images loom keeps as artifacts.
type CallResult struct {
	Text    string
	Sources []Source
	Files   []File
	Images  []Image
	// DroppedImages counts the image blocks loom refused (type, size, count),
	// so the model is told rather than claiming a picture that does not exist.
	DroppedImages int
}

// Image is a picture a tool result carried as MCP image content, for loom to
// keep as an artifact. Only a server with ServerConfig.Artifacts gets one
// through. The rest is display detail from structuredContent.images, matched
// by position: images[i] describes the result's i-th image block.
type Image struct {
	Filename string
	// MimeType is canonical (artifact.CanonicalImageMIME); Extension matches it.
	MimeType   string
	Extension  string
	Data       []byte
	Model      string
	Provider   string
	Width      int
	Height     int
	DurationMs int64
}

// imageMeta is one entry of structuredContent.images.
type imageMeta struct {
	Filename   string `json:"filename"`
	Model      string `json:"model"`
	Provider   string `json:"provider"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMs int64  `json:"duration_ms"`
}

// maxToolImages caps how many images one tool result may hand on. Together
// they stay within one artifact's size (artifact.MaxArtifactSizeBytes): the
// reply cap (maxArtifactRPCBytes) fits that much and no more.
const maxToolImages = 4

// File is data a tool result carried as an MCP embedded resource, for
// run_python to read during the running turn. Never shown, never part of the
// prompt; loom keeps it for that turn only.
type File struct {
	Name     string
	MimeType string
	Data     []byte
}

// maxToolFiles caps how many files one tool result may hand on.
const maxToolFiles = 10

// toolFileTypes are the resource types loom hands to run_python, by the
// extension the name must carry.
var toolFileTypes = map[string]string{"text/csv": ".csv", "application/json": ".json"}

type callToolResult struct {
	Content           []toolContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

type toolContent struct {
	Type     string        `json:"type"`
	Text     string        `json:"text"`
	Resource *toolResource `json:"resource"`
	// Data and MimeType belong to an image block: base64 and its type.
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

type toolResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// toCallResult flattens a decoded tools/call result. A structuredContent that
// is not an object with a sources or images array is ignored: the 2026-07-28
// revision allows any JSON value there, and only this shape means something to
// loom. images keeps the image blocks; false drops them (a server without
// ServerConfig.Artifacts).
func (r callToolResult) toCallResult(images bool) CallResult {
	out := CallResult{Text: toolContentText(r.Content), Sources: structuredSources(r.StructuredContent), Files: toolFiles(r.Content)}
	if images {
		out.Images, out.DroppedImages = toolImages(r.Content, structuredImageMeta(r.StructuredContent), artifact.MaxArtifactSizeBytes)
	}
	return out
}

// toolImages decodes the image blocks loom keeps: an accepted raster type
// whose bytes are that type, at most limit bytes decoded for all of them
// together, at most maxToolImages. It also counts the blocks it refused.
// meta[i] describes the i-th image block, kept or not, so a dropped block
// never shifts its details onto the next one.
func toolImages(content []toolContent, meta []imageMeta, limit int) ([]Image, int) {
	var out []Image
	dropped, block, total := 0, -1, 0
	for _, item := range content {
		if item.Type != "image" {
			continue
		}
		block++
		if len(out) == maxToolImages {
			dropped++
			continue
		}
		img, ok := decodeToolImage(item, limit-total)
		if !ok {
			dropped++
			continue
		}
		total += len(img.Data)
		if block < len(meta) {
			m := meta[block]
			img.Filename, img.Model, img.Provider = baseName(m.Filename), m.Model, m.Provider
			img.Width, img.Height, img.DurationMs = m.Width, m.Height, m.DurationMs
		}
		out = append(out, img)
	}
	if dropped > 0 {
		slog.Warn("MCP image content dropped", "dropped", dropped, "kept", len(out))
	}
	return out, dropped
}

// decodeToolImage returns an image block as an Image when its type is an
// accepted raster type (SVG is not: it is a document that can carry script),
// it fits limit and its bytes sniff as that type: a server cannot store HTML
// under image/png.
func decodeToolImage(item toolContent, limit int) (Image, bool) {
	if item.Data == "" || base64.StdEncoding.DecodedLen(len(item.Data)) > limit+2 {
		return Image{}, false
	}
	data, err := base64.StdEncoding.DecodeString(item.Data)
	if err != nil {
		return Image{}, false
	}
	return checkImage(item.MimeType, data, limit)
}

// checkImage is the one gate every image passes before it can become an
// artifact, whoever produced it: an accepted raster type, non-empty, within
// limit, bytes that sniff as that type.
func checkImage(mimeType string, data []byte, limit int) (Image, bool) {
	mime, ext, ok := artifact.CanonicalImageMIME(mimeType)
	if !ok || len(data) == 0 || len(data) > limit || http.DetectContentType(data) != mime {
		return Image{}, false
	}
	return Image{MimeType: mime, Extension: ext, Data: data}, true
}

// baseName keeps only the last element of a server's file name: a server
// that reports the path it wrote ("/tmp/out/cat.png") still names the file.
func baseName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	return name
}

func structuredImageMeta(raw json.RawMessage) []imageMeta {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Images []json.RawMessage `json:"images"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	// Entry by entry: one mistyped field (a width sent as a string) costs
	// that image its details, not every image in the reply.
	out := make([]imageMeta, len(payload.Images))
	for i, entry := range payload.Images {
		_ = json.Unmarshal(entry, &out[i])
	}
	return out
}

// toolFiles keeps the text resources run_python can use: CSV or JSON, under
// a plain file name of the matching extension (file:///name.csv). Anything
// else (a path, a blob, another type) is dropped: the name becomes a file in
// the sandbox's input directory.
func toolFiles(content []toolContent) []File {
	var out []File
	for _, item := range content {
		if item.Type != "resource" || item.Resource == nil || item.Resource.Text == "" || len(out) == maxToolFiles {
			continue
		}
		ext, ok := toolFileTypes[item.Resource.MimeType]
		name := strings.TrimPrefix(item.Resource.URI, "file:///")
		if !ok || !toolFileName.MatchString(name) || strings.Contains(name, "..") || !strings.HasSuffix(name, ext) {
			continue
		}
		out = append(out, File{Name: name, MimeType: item.Resource.MimeType, Data: []byte(item.Resource.Text)})
	}
	return out
}

// toolFileName is a plain file name the sandbox accepts (its safeName): no
// directory, no leading dot.
var toolFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func structuredSources(raw json.RawMessage) []Source {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Sources []Source `json:"sources"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	// Positions are kept, unusable entries included: sources[i] belongs to the
	// text's item i+1, so dropping one would shift every later citation onto
	// the wrong item. The consumer skips an entry it cannot register.
	sources := payload.Sources
	if len(sources) > maxStructuredSources {
		sources = sources[:maxStructuredSources]
	}
	usable := false
	for i := range sources {
		sources[i].URL = strings.TrimSpace(sources[i].URL)
		usable = usable || sources[i].URL != ""
	}
	if !usable {
		return nil
	}
	return sources
}

func toolContentText(content []toolContent) string {
	var builder strings.Builder
	for _, item := range content {
		if item.Type == "text" && item.Text != "" {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			remaining := maxToolOutputBytes - builder.Len()
			if remaining <= 0 {
				break
			}
			if len(item.Text) > remaining {
				builder.WriteString(item.Text[:remaining])
				break
			}
			builder.WriteString(item.Text)
		}
	}
	return builder.String()
}
