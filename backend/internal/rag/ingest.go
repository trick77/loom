package rag

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// embedBatchSize bounds how many chunks are embedded per request.
const embedBatchSize = 64

// Extractor turns a document's raw bytes into plain text (implemented by
// documents.TikaClient).
type Extractor interface {
	Extract(ctx context.Context, filename, mime string, r io.Reader) (string, error)
}

// Embedder turns texts into vectors (implemented by EmbedClient).
type Embedder interface {
	Embed(ctx context.Context, inputs []string) (EmbedResult, error)
}

type EmbeddingUsageRecorder interface {
	AddEmbeddingUsage(ctx context.Context, userID string, tokens, requests int) error
}

// FileOpener opens a document's bytes from the per-user volume (sandboxed).
type FileOpener interface {
	OpenDocument(d Document) (io.ReadCloser, error)
}

// Ingester runs the extract -> chunk -> embed -> store pipeline for a document.
type Ingester struct {
	store     *Store
	opener    FileOpener
	extractor Extractor
	embedder  Embedder
	chunkOpts ChunkOptions
	usage     EmbeddingUsageRecorder
}

func NewIngester(store *Store, opener FileOpener, extractor Extractor, embedder Embedder) *Ingester {
	return &Ingester{
		store:     store,
		opener:    opener,
		extractor: extractor,
		embedder:  embedder,
		chunkOpts: DefaultChunkOptions(),
	}
}

func (ing *Ingester) SetUsageRecorder(usage EmbeddingUsageRecorder) {
	ing.usage = usage
}

// Ingest indexes one document. On any failure it records the error on the
// document (status=error) and returns it, so the caller need not also persist.
func (ing *Ingester) Ingest(ctx context.Context, userID, documentID string) error {
	doc, ok, err := ing.store.GetDocument(ctx, userID, documentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("document %s not found for user", documentID)
	}

	text, err := ing.extract(ctx, doc)
	if err != nil {
		return ing.fail(ctx, userID, documentID, err)
	}
	if strings.TrimSpace(text) == "" {
		return ing.fail(ctx, userID, documentID, fmt.Errorf("no extractable text"))
	}

	// Cache the extracted text so the inline/full-document path reads it back
	// instead of re-running Tika on every turn. Best-effort: a cache-write failure
	// only means that path falls back to live extraction, so it must not fail ingest.
	_ = ing.store.SetDocumentFullText(ctx, userID, documentID, text)

	_ = ing.store.UpdateStatus(ctx, userID, documentID, StatusEmbedding, "")
	chunks := Chunk(text, ing.chunkOpts)
	embeddings, err := ing.embedAll(ctx, userID, chunks)
	if err != nil {
		return ing.fail(ctx, userID, documentID, err)
	}

	if err := ing.store.ReplaceChunks(ctx, userID, documentID, chunks, embeddings); err != nil {
		return ing.fail(ctx, userID, documentID, err)
	}
	return nil
}

// ExtractText returns a document's full plain text by re-running extraction from
// the volume, without touching the document's status or its chunks/embeddings.
// It backs the inline-attachment path (full text in the prompt) and works
// regardless of whether the document has been indexed for RAG.
func (ing *Ingester) ExtractText(ctx context.Context, userID, documentID string) (string, error) {
	// Prefer the text cached at ingestion; fall back to live extraction for
	// documents not yet indexed (e.g. attachments) or indexed before caching.
	if cached, err := ing.store.GetDocumentFullText(ctx, userID, documentID); err != nil {
		return "", err
	} else if strings.TrimSpace(cached) != "" {
		return cached, nil
	}
	doc, ok, err := ing.store.GetDocument(ctx, userID, documentID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("document %s not found for user", documentID)
	}
	rc, err := ing.opener.OpenDocument(doc)
	if err != nil {
		return "", fmt.Errorf("open document: %w", err)
	}
	defer rc.Close()
	text, err := ing.extractor.Extract(ctx, doc.Filename, doc.MIME, rc)
	if err != nil {
		return "", fmt.Errorf("extract text: %w", err)
	}
	return text, nil
}

func (ing *Ingester) extract(ctx context.Context, doc Document) (string, error) {
	_ = ing.store.UpdateStatus(ctx, doc.UserID, doc.ID, StatusExtracting, "")
	rc, err := ing.opener.OpenDocument(doc)
	if err != nil {
		return "", fmt.Errorf("open document: %w", err)
	}
	defer rc.Close()
	text, err := ing.extractor.Extract(ctx, doc.Filename, doc.MIME, rc)
	if err != nil {
		return "", fmt.Errorf("extract text: %w", err)
	}
	return text, nil
}

// embedAll embeds chunk texts in bounded batches and returns vectors aligned to
// the chunk order.
func (ing *Ingester) embedAll(ctx context.Context, userID string, chunks []TextChunk) ([][]float32, error) {
	embeddings := make([][]float32, 0, len(chunks))
	var usageTokens int
	var usageRequests int
	defer func() {
		ing.recordEmbeddingUsage(ctx, userID, usageTokens, usageRequests)
	}()
	for start := 0; start < len(chunks); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		inputs := make([]string, end-start)
		for i := range inputs {
			inputs[i] = chunks[start+i].Text
		}
		result, err := ing.embedder.Embed(ctx, inputs)
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		embeddings = append(embeddings, result.Vectors...)
		if result.Usage.Present {
			usageTokens += result.Usage.TotalTokens
			usageRequests++
		}
	}
	return embeddings, nil
}

func (ing *Ingester) recordEmbeddingUsage(ctx context.Context, userID string, tokens, requests int) {
	if ing.usage == nil || tokens == 0 || requests == 0 {
		return
	}
	_ = ing.usage.AddEmbeddingUsage(ctx, userID, tokens, requests)
}

func (ing *Ingester) fail(ctx context.Context, userID, documentID string, cause error) error {
	_ = ing.store.UpdateStatus(ctx, userID, documentID, StatusError, cause.Error())
	return cause
}
