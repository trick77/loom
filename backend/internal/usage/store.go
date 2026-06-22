// Package usage owns per-user lifetime usage counters. The totals live in one
// row per user (user_usage_totals) and are mutated additively, so they survive
// deletion of the chats/projects that produced them. All writes are best-effort
// from the caller's perspective: callers log and swallow errors so a counter
// failure never fails the underlying request.
package usage

import (
	"context"
	"database/sql"
	"errors"
)

// DBTX is the minimal database surface the store needs (satisfied by *sql.DB).
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// TokenDelta is one turn's token usage to add to a user's lifetime totals.
type TokenDelta struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	ReasoningTokens  int
	TotalTokens      int
}

// Totals is a user's lifetime usage. JSON tags match the frontend Usage type.
type Totals struct {
	PromptTokens      int `json:"promptTokens"`
	CompletionTokens  int `json:"completionTokens"`
	CachedTokens      int `json:"cachedTokens"`
	ReasoningTokens   int `json:"reasoningTokens"`
	TotalTokens       int `json:"totalTokens"`
	EmbeddingTokens   int `json:"embeddingTokens"`
	EmbeddingRequests int `json:"embeddingRequests"`
	WebSearches       int `json:"webSearches"`
	WebFetches        int `json:"webFetches"`
	ObscuraFetches    int `json:"obscuraFetches"`
	ImageGens         int `json:"imageGens"`
	ThreadsCreated    int `json:"threadsCreated"`
	ProjectsCreated   int `json:"projectsCreated"`
}

type Store struct{ db DBTX }

func NewStore(db DBTX) *Store { return &Store{db: db} }

// AddTokens adds one turn's token usage to the user's lifetime totals, creating
// the row on first use.
func (s *Store) AddTokens(ctx context.Context, userID string, d TokenDelta) error {
	const q = `INSERT INTO user_usage_totals
		(user_id, prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens, total_tokens)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			prompt_tokens     = prompt_tokens + excluded.prompt_tokens,
			completion_tokens = completion_tokens + excluded.completion_tokens,
			cached_tokens     = cached_tokens + excluded.cached_tokens,
			reasoning_tokens  = reasoning_tokens + excluded.reasoning_tokens,
			total_tokens      = total_tokens + excluded.total_tokens,
			updated_at        = datetime('now')`
	_, err := s.db.ExecContext(ctx, q, userID,
		d.PromptTokens, d.CompletionTokens, d.CachedTokens, d.ReasoningTokens, d.TotalTokens)
	return err
}

func (s *Store) AddEmbeddingUsage(ctx context.Context, userID string, tokens, requests int) error {
	const q = `INSERT INTO user_usage_totals
		(user_id, embedding_tokens, embedding_requests)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			embedding_tokens   = embedding_tokens + excluded.embedding_tokens,
			embedding_requests = embedding_requests + excluded.embedding_requests,
			updated_at         = datetime('now')`
	_, err := s.db.ExecContext(ctx, q, userID, tokens, requests)
	return err
}

func (s *Store) IncWebSearch(ctx context.Context, userID string) error {
	return s.bump(ctx, userID, "web_searches")
}
func (s *Store) IncWebFetch(ctx context.Context, userID string) error {
	return s.bump(ctx, userID, "web_fetches")
}
func (s *Store) IncObscuraFetch(ctx context.Context, userID string) error {
	return s.bump(ctx, userID, "obscura_fetches")
}
func (s *Store) IncImageGen(ctx context.Context, userID string) error {
	return s.bump(ctx, userID, "image_gens")
}
func (s *Store) IncThreadCreated(ctx context.Context, userID string) error {
	return s.bump(ctx, userID, "chats_created")
}
func (s *Store) IncProjectCreated(ctx context.Context, userID string) error {
	return s.bump(ctx, userID, "projects_created")
}

// bump adds 1 to a single counter column. column is always a compile-time
// constant from this package — never user input — so building the query string
// here is safe from injection.
func (s *Store) bump(ctx context.Context, userID, column string) error {
	q := "INSERT INTO user_usage_totals (user_id, " + column + ") VALUES (?, 1) " +
		"ON CONFLICT(user_id) DO UPDATE SET " + column + " = " + column + " + 1, updated_at = datetime('now')"
	_, err := s.db.ExecContext(ctx, q, userID)
	return err
}

// Get returns the user's lifetime totals, or a zero Totals if no row exists yet.
func (s *Store) Get(ctx context.Context, userID string) (Totals, error) {
	const q = `SELECT prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens, total_tokens,
		embedding_tokens, embedding_requests,
		web_searches, web_fetches, obscura_fetches, image_gens, chats_created, projects_created
		FROM user_usage_totals WHERE user_id = ?`
	var t Totals
	err := s.db.QueryRowContext(ctx, q, userID).Scan(
		&t.PromptTokens, &t.CompletionTokens, &t.CachedTokens, &t.ReasoningTokens, &t.TotalTokens,
		&t.EmbeddingTokens, &t.EmbeddingRequests,
		&t.WebSearches, &t.WebFetches, &t.ObscuraFetches, &t.ImageGens, &t.ThreadsCreated, &t.ProjectsCreated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Totals{}, nil
	}
	if err != nil {
		return Totals{}, err
	}
	return t, nil
}
