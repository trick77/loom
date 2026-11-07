package mcp

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
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
}

// Image is a picture a tool result carried as MCP image content, for loom to
// keep as an artifact. Only a server with ServerConfig.Artifacts gets one
// through. The rest is display detail from structuredContent.images, matched
// by position: images[i] describes the result's i-th image block.
type Image struct {
	Filename   string
	MimeType   string
	Data       []byte
	Model      string
	Provider   string
	Width      int
	Height     int
	DurationMs int64
}

// Extension is the file extension (no dot) the image's type is stored under.
func (i Image) Extension() string {
	return toolImageTypes[i.MimeType]
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

const (
	// maxToolImages caps how many images one tool result may hand on.
	maxToolImages = 4
	// maxToolImageBytes is the largest image kept, decoded: the artifact
	// size limit (artifact.MaxArtifactSizeBytes).
	maxToolImageBytes = 25 << 20
)

// toolImageTypes are the raster types loom keeps, with their extension. SVG
// is left out on purpose: it is a document that can carry script.
var toolImageTypes = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/gif": "gif"}

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
		out.Images = toolImages(r.Content, structuredImageMeta(r.StructuredContent), maxToolImageBytes)
	}
	return out
}

// toolImages decodes the image blocks loom keeps: an allowed raster type
// whose bytes are that type, at most limit bytes decoded, at most
// maxToolImages per result. meta[i] describes the i-th image block, kept or
// not, so a dropped block never shifts its details onto the next one.
func toolImages(content []toolContent, meta []imageMeta, limit int) []Image {
	var out []Image
	block := -1
	for _, item := range content {
		if item.Type != "image" {
			continue
		}
		block++
		if len(out) == maxToolImages {
			break
		}
		data, ok := decodeToolImage(item, limit)
		if !ok {
			continue
		}
		img := Image{MimeType: item.MimeType, Data: data}
		if block < len(meta) {
			m := meta[block]
			img.Filename, img.Model, img.Provider = strings.TrimSpace(m.Filename), m.Model, m.Provider
			img.Width, img.Height, img.DurationMs = m.Width, m.Height, m.DurationMs
		}
		out = append(out, img)
	}
	return out
}

// decodeToolImage returns an image block's bytes when its type is an allowed
// raster type, they fit limit and they sniff as the declared type: a server
// cannot store HTML under image/png.
func decodeToolImage(item toolContent, limit int) ([]byte, bool) {
	if _, ok := toolImageTypes[item.MimeType]; !ok || item.Data == "" || base64.StdEncoding.DecodedLen(len(item.Data)) > limit+2 {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(item.Data)
	if err != nil || len(data) == 0 || len(data) > limit || http.DetectContentType(data) != item.MimeType {
		return nil, false
	}
	return data, true
}

func structuredImageMeta(raw json.RawMessage) []imageMeta {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Images []imageMeta `json:"images"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload.Images
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
