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
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
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

	_ = ing.store.UpdateStatus(ctx, userID, documentID, StatusEmbedding, "")
	chunks := Chunk(text, ing.chunkOpts)
	embeddings, err := ing.embedAll(ctx, chunks)
	if err != nil {
		return ing.fail(ctx, userID, documentID, err)
	}

	if err := ing.store.ReplaceChunks(ctx, userID, documentID, chunks, embeddings); err != nil {
		return ing.fail(ctx, userID, documentID, err)
	}
	return nil
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
func (ing *Ingester) embedAll(ctx context.Context, chunks []TextChunk) ([][]float32, error) {
	embeddings := make([][]float32, 0, len(chunks))
	for start := 0; start < len(chunks); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		inputs := make([]string, end-start)
		for i := range inputs {
			inputs[i] = chunks[start+i].Text
		}
		vecs, err := ing.embedder.Embed(ctx, inputs)
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		embeddings = append(embeddings, vecs...)
	}
	return embeddings, nil
}

func (ing *Ingester) fail(ctx context.Context, userID, documentID string, cause error) error {
	_ = ing.store.UpdateStatus(ctx, userID, documentID, StatusError, cause.Error())
	return cause
}
