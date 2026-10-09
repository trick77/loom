package turn

// Citation mirrors AnythingLLM's source model: one entry per retrieved chunk
// (filename = document title, snippet = matched text, score = similarity). The
// frontend groups these by filename for display ("combine like sources").
type Citation struct {
	DocumentID string  `json:"documentId"`
	Filename   string  `json:"filename"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
	// Full marks a source whose entire document was injected (not a retrieved
	// excerpt), so the UI can label it "full document" instead of "N excerpts".
	Full bool `json:"full,omitempty"`
	// URL and Index are set for web-search citations (Tavily/fetch/obscura): URL
	// is the source link and Index is the [n] marker the model cites inline. RAG
	// document citations leave both zero-valued. The frontend distinguishes a web
	// source by the presence of url. For web citations Filename holds the display
	// label (the site name), not a document filename.
	URL   string `json:"url,omitempty"`
	Index int    `json:"index,omitempty"`
	// Title and Favicon are web-citation extras for the sources sidebar: Title is
	// the page/article title (Snippet carries what the source delivered), Favicon
	// is a source-provided icon URL when available (the frontend otherwise derives
	// one). Empty for RAG document citations.
	Title   string `json:"title,omitempty"`
	Favicon string `json:"favicon,omitempty"`
}
