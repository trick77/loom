package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/trick77/loom/internal/artifact"
)

// maxStructuredSources caps how many sources one tool result may contribute, so
// a misbehaving server cannot flood the sources sidebar.
const maxStructuredSources = 20

// Source is one citable item a tool result carries in
// structuredContent.sources: the convention loom reads so any MCP server can
// feed the sources sidebar and the model's [n] markers. URL is required; the
// rest is display detail; Image is a preview picture (a video thumbnail);
// Product makes the link a product page, shown as a card under the answer.
type Source struct {
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Label   string   `json:"label"`
	Snippet string   `json:"snippet"`
	Image   string   `json:"image"`
	Product *Product `json:"product,omitempty"`
}

// Product is what a shop knows about a product page (docs/shop.md). A nil
// Price is no price known; it comes with its ISO 4217 Currency or not at all.
// Rating is an average out of 5, 0 none.
type Product struct {
	Price       *float64 `json:"price,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	RatingCount int      `json:"ratingCount,omitempty"`
}

// maxProductRating is the top of the scale a Rating is read on.
const maxProductRating = 5

// currencyCode is an ISO 4217 code, upper-cased before the match.
var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// cleanProduct keeps what a card can show truthfully and drops the rest: a
// price that is not above zero (zero is "none", the contract's nil) or comes
// without a valid currency, a rating off the scale, a count without its
// rating. Nothing left is no product.
func cleanProduct(p *Product) *Product {
	if p == nil {
		return nil
	}
	out := Product{}
	currency := strings.ToUpper(strings.TrimSpace(p.Currency))
	if p.Price != nil && *p.Price > 0 && currencyCode.MatchString(currency) {
		price := *p.Price
		out.Price, out.Currency = &price, currency
	}
	if p.Rating > 0 && p.Rating <= maxProductRating {
		out.Rating = p.Rating
		if p.RatingCount > 0 {
			out.RatingCount = p.RatingCount
		}
	}
	if out == (Product{}) {
		return nil
	}
	return &out
}

// CallResult is a tool call's model-facing text plus the sources it cited,
// the files it handed to run_python and the images and documents loom keeps
// as artifacts.
type CallResult struct {
	Text      string
	Sources   []Source
	Files     []File
	Images    []Image
	Documents []Document
	// DroppedImages and DroppedDocuments count the image blocks and blob
	// resources loom refused (type, size, count), so the model is told
	// rather than claiming a file that does not exist.
	DroppedImages    int
	DroppedDocuments int
}

// Document is a file a tool result carried as an MCP blob resource, for loom
// to keep as an artifact (a generated PDF, spreadsheet, text file). Only a
// server with ServerConfig.Artifacts gets one through, and only of a type
// loom generates (documentTypes).
type Document struct {
	// Name is the server's plain file name; Extension is its type's, lower
	// case. The stored type is loom's own for it (artifact.MIMEType).
	Name      string
	Extension string
	Data      []byte
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

// maxToolArtifacts caps how many images and documents one tool result may
// hand on. Together they stay within one artifact's size
// (artifact.MaxArtifactSizeBytes): the reply cap (maxArtifactRPCBytes) fits
// that much and no more.
const maxToolArtifacts = 4

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
	// Blob is a binary resource's bytes, base64.
	Blob string `json:"blob"`
}

// toCallResult flattens a decoded tools/call result. A structuredContent that
// is not an object with a sources or images array is ignored: the 2026-07-28
// revision allows any JSON value there, and only this shape means something to
// loom. artifacts keeps the image blocks and blob resources; false drops them
// (a server without ServerConfig.Artifacts).
func (r callToolResult) toCallResult(artifacts bool) CallResult {
	out := CallResult{Text: toolContentText(r.Content), Sources: structuredSources(r.StructuredContent), Files: toolFiles(r.Content)}
	if artifacts {
		kept := toolArtifacts(r.Content, structuredImageMeta(r.StructuredContent), artifact.MaxArtifactSizeBytes)
		out.Images, out.Documents, out.DroppedImages, out.DroppedDocuments = kept.images, kept.documents, kept.droppedImages, kept.droppedDocuments
	}
	return out
}

// keptArtifacts is what toolArtifacts keeps of a result, and how much it
// refused.
type keptArtifacts struct {
	images                          []Image
	documents                       []Document
	droppedImages, droppedDocuments int
}

// toolArtifacts decodes the image blocks and blob resources loom keeps, in
// content order: each of a type it accepts whose bytes are that type, at most
// limit bytes decoded for all of them together, at most maxToolArtifacts.
// It also counts the ones it refused. meta[i] describes the i-th image block,
// kept or not, so a dropped block never shifts its details onto the next one.
func toolArtifacts(content []toolContent, meta []imageMeta, limit int) keptArtifacts {
	var out keptArtifacts
	block, total := -1, 0
	full := func() bool { return len(out.images)+len(out.documents) == maxToolArtifacts }
	for _, item := range content {
		switch {
		case item.Type == "image":
			block++
			img, ok := Image{}, false
			if !full() {
				img, ok = decodeToolImage(item, limit-total)
			}
			if !ok {
				out.droppedImages++
				continue
			}
			total += len(img.Data)
			if block < len(meta) {
				m := meta[block]
				img.Filename, img.Model, img.Provider = baseName(m.Filename), m.Model, m.Provider
				img.Width, img.Height, img.DurationMs = m.Width, m.Height, m.DurationMs
			}
			out.images = append(out.images, img)
		case item.Type == "resource" && item.Resource != nil && item.Resource.Blob != "":
			doc, ok := Document{}, false
			if !full() {
				doc, ok = decodeToolDocument(*item.Resource, limit-total)
			}
			if !ok {
				out.droppedDocuments++
				continue
			}
			total += len(doc.Data)
			out.documents = append(out.documents, doc)
		}
	}
	if out.droppedImages > 0 || out.droppedDocuments > 0 {
		slog.Warn("MCP artifact content dropped", "dropped_images", out.droppedImages, "dropped_documents", out.droppedDocuments,
			"kept_images", len(out.images), "kept_documents", len(out.documents))
	}
	return out
}

// decodeToolImage returns an image block as an Image when its type is an
// accepted raster type (SVG is not: it is a document that can carry script),
// it fits limit and its bytes sniff as that type: a server cannot store HTML
// under image/png.
func decodeToolImage(item toolContent, limit int) (Image, bool) {
	data, ok := decodeBase64(item.Data, limit)
	if !ok {
		return Image{}, false
	}
	return checkImage(item.MimeType, data, limit)
}

// decodeBase64 decodes standard base64 of at most limit bytes; a longer one
// is refused before it is decoded.
func decodeBase64(encoded string, limit int) ([]byte, bool) {
	if encoded == "" || base64.StdEncoding.DecodedLen(len(encoded)) > limit+2 {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > limit {
		return nil, false
	}
	return data, true
}

// documentTypes are the documents loom keeps from a blob resource, by
// extension: the formats loom's document tools generate (docs/docgen.md;
// loom-docgen's tests hold them to docgen.Extensions), each with the check
// its bytes must pass. A server cannot store HTML under a PDF name, nor a
// binary as text.
var documentTypes = map[string]func([]byte) bool{
	"pdf":  hasPrefix("%PDF-"),
	"docx": hasPrefix("PK\x03\x04"),
	"xlsx": hasPrefix("PK\x03\x04"),
	"pptx": hasPrefix("PK\x03\x04"),
	"txt":  isText, "md": isText, "csv": isText, "json": isText, "html": isText,
	"xml": isText, "yaml": isText, "yml": isText, "log": isText,
}

func hasPrefix(magic string) func([]byte) bool {
	return func(data []byte) bool { return bytes.HasPrefix(data, []byte(magic)) }
}

// isText accepts UTF-8 without NUL bytes.
func isText(data []byte) bool {
	return utf8.Valid(data) && bytes.IndexByte(data, 0) < 0
}

// decodeToolDocument returns a blob resource as a Document when its name's
// extension is a document type, it fits limit and its bytes pass the type's
// check. The resource's own mimeType decides nothing: loom stores the file
// under its type for the extension (artifact.MIMEType), so a server that
// spells a type differently (application/yaml, octet-stream) loses nothing.
func decodeToolDocument(res toolResource, limit int) (Document, bool) {
	name := baseName(strings.TrimPrefix(res.URI, "file://"))
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	check, ok := documentTypes[ext]
	if !ok {
		return Document{}, false
	}
	data, ok := decodeBase64(res.Blob, limit)
	if !ok || !check(data) {
		return Document{}, false
	}
	return Document{Name: name, Extension: ext, Data: data}, true
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
		sources[i].Product = cleanProduct(sources[i].Product)
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
