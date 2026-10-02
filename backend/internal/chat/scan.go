package chat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

type rowScanner interface {
	Scan(...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	var archivedAt, autoDescriptionGeneratedAt sql.NullString
	var createdAt, updatedAt, lastActivityAt string
	if err := row.Scan(&project.ID, &project.UserID, &project.Name, &project.Description, &project.Starred, &archivedAt, &autoDescriptionGeneratedAt, &project.DescriptionUserEdited, &project.DescriptionSourceThreadCount, &createdAt, &updatedAt, &lastActivityAt); err != nil {
		return Project{}, err
	}
	var err error
	project.ArchivedAt, err = nullableTime(archivedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse archived_at: %w", err)
	}
	project.AutoDescriptionGeneratedAt, err = nullableTime(autoDescriptionGeneratedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse auto_description_generated_at: %w", err)
	}
	project.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse created_at: %w", err)
	}
	project.UpdatedAt, err = sqlutil.ParseTime(updatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse updated_at: %w", err)
	}
	project.LastActivityAt, err = sqlutil.ParseTime(lastActivityAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse last_activity_at: %w", err)
	}
	return project, nil
}

func scanThread(row rowScanner) (Thread, error) {
	return scanThreadRow(row)
}

// scanThreadWithSnippet scans a thread row followed by a trailing snippet
// column (the SELECT order used by SearchThreadsByContent).
func scanThreadWithSnippet(row rowScanner) (Thread, string, error) {
	var snippet string
	thread, err := scanThreadRow(row, &snippet)
	return thread, snippet, err
}

// scanThreadRow scans the thread columns in their SELECT order, then any extra
// trailing columns the query added.
func scanThreadRow(row rowScanner, extra ...any) (Thread, error) {
	var thread Thread
	var projectID sql.NullString
	var archivedAt, lastMessageAt sql.NullString
	var createdAt, updatedAt string
	dest := []any{&thread.ID, &thread.UserID, &projectID, &thread.Title, &thread.Category, &thread.ImageModel, &thread.Starred, &archivedAt, &createdAt, &updatedAt, &lastMessageAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return Thread{}, err
	}
	if projectID.Valid {
		thread.ProjectID = &projectID.String
	}
	var err error
	thread.ArchivedAt, err = nullableTime(archivedAt)
	if err != nil {
		return Thread{}, fmt.Errorf("parse archived_at: %w", err)
	}
	thread.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return Thread{}, fmt.Errorf("parse created_at: %w", err)
	}
	thread.UpdatedAt, err = sqlutil.ParseTime(updatedAt)
	if err != nil {
		return Thread{}, fmt.Errorf("parse updated_at: %w", err)
	}
	thread.LastMessageAt, err = nullableTime(lastMessageAt)
	if err != nil {
		return Thread{}, fmt.Errorf("parse last_message_at: %w", err)
	}
	return thread, nil
}

// messageColumns is the full messages projection scanMessage reads.
const messageColumns = "id, thread_id, role, content, reasoning_content, tool_calls, citations, artifacts, attachments, pasted_texts, activity_trace, content_blocks, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, context_tokens, cost_nano_usd, duration_ms, model, reasoning_effort, created_at"

// transcriptColumns is the narrow projection for callers that only render a
// "Role: content" transcript (thread digests, memory generation). It leaves out
// the reasoning, trace and content-block columns, which carry several copies of
// every answer and are the bulk of a row.
const transcriptColumns = "id, thread_id, role, content, created_at"

// transcriptColumnsM is transcriptColumns for a query that aliases messages as m.
const transcriptColumnsM = "m.id, m.thread_id, m.role, m.content, m.created_at"

// scanTranscriptMessage reads a transcriptColumns row. Only ID, ThreadID, Role,
// Content and CreatedAt are set on the result.
func scanTranscriptMessage(row rowScanner) (Message, error) {
	var message Message
	var role, createdAt string
	if err := row.Scan(&message.ID, &message.ThreadID, &role, &message.Content, &createdAt); err != nil {
		return Message{}, err
	}
	message.Role = Role(role)
	var err error
	message.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return Message{}, fmt.Errorf("parse created_at: %w", err)
	}
	return message, nil
}

func scanMessage(row rowScanner) (Message, error) {
	var message Message
	var role string
	var toolCalls, citations, artifacts, attachments, pastedTexts, activityTrace, contentBlocks string
	var promptTokens, completionTokens, totalTokens, cachedTokens, reasoningTokens, contextTokens, costNanoUSD, durationMs sql.NullInt64
	var model, reasoningEffort sql.NullString
	var createdAt string
	if err := row.Scan(
		&message.ID,
		&message.ThreadID,
		&role,
		&message.Content,
		&message.ReasoningContent,
		&toolCalls,
		&citations,
		&artifacts,
		&attachments,
		&pastedTexts,
		&activityTrace,
		&contentBlocks,
		&promptTokens,
		&completionTokens,
		&totalTokens,
		&cachedTokens,
		&reasoningTokens,
		&contextTokens,
		&costNanoUSD,
		&durationMs,
		&model,
		&reasoningEffort,
		&createdAt,
	); err != nil {
		return Message{}, err
	}
	message.Role = Role(role)
	message.ToolCalls = defaultJSON(toolCalls)
	message.Citations = defaultJSON(citations)
	message.Artifacts = defaultJSON(artifacts)
	message.Attachments = defaultJSON(attachments)
	message.PastedTexts = defaultJSON(pastedTexts)
	message.ActivityTrace = defaultJSON(activityTrace)
	message.ContentBlocks = defaultJSON(contentBlocks)
	message.PromptTokens = nullableInt(promptTokens)
	message.CompletionTokens = nullableInt(completionTokens)
	message.TotalTokens = nullableInt(totalTokens)
	message.CachedTokens = nullableInt(cachedTokens)
	message.ReasoningTokens = nullableInt(reasoningTokens)
	message.ContextTokens = nullableInt(contextTokens)
	if costNanoUSD.Valid {
		v := costNanoUSD.Int64
		message.CostNanoUSD = &v
	}
	message.DurationMs = nullableInt(durationMs)
	message.Model = nullableString(model)
	message.ReasoningEffort = nullableString(reasoningEffort)
	var err error
	message.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return Message{}, fmt.Errorf("parse created_at: %w", err)
	}
	return message, nil
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	v := int(value.Int64)
	return &v
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func nullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := sqlutil.ParseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func defaultJSON(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "[]"
	}
	return json.RawMessage(value)
}
