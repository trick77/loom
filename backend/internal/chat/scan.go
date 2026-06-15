package chat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type rowScanner interface {
	Scan(...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	var archivedAt, autoDescriptionGeneratedAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&project.ID, &project.UserID, &project.Name, &project.Description, &project.Starred, &archivedAt, &autoDescriptionGeneratedAt, &createdAt, &updatedAt); err != nil {
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
	project.CreatedAt, err = parseSQLiteTime(createdAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse created_at: %w", err)
	}
	project.UpdatedAt, err = parseSQLiteTime(updatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return project, nil
}

func scanThread(row rowScanner) (Thread, error) {
	var thread Thread
	var projectID sql.NullString
	var archivedAt, lastMessageAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&thread.ID, &thread.UserID, &projectID, &thread.Title, &thread.Starred, &archivedAt, &createdAt, &updatedAt, &lastMessageAt); err != nil {
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
	thread.CreatedAt, err = parseSQLiteTime(createdAt)
	if err != nil {
		return Thread{}, fmt.Errorf("parse created_at: %w", err)
	}
	thread.UpdatedAt, err = parseSQLiteTime(updatedAt)
	if err != nil {
		return Thread{}, fmt.Errorf("parse updated_at: %w", err)
	}
	thread.LastMessageAt, err = nullableTime(lastMessageAt)
	if err != nil {
		return Thread{}, fmt.Errorf("parse last_message_at: %w", err)
	}
	return thread, nil
}

func scanMessage(row rowScanner) (Message, error) {
	var message Message
	var role string
	var toolCalls, citations, artifacts, attachments, activityTrace string
	var promptTokens, completionTokens, totalTokens, cachedTokens, reasoningTokens, durationMs sql.NullInt64
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
		&activityTrace,
		&promptTokens,
		&completionTokens,
		&totalTokens,
		&cachedTokens,
		&reasoningTokens,
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
	message.ActivityTrace = defaultJSON(activityTrace)
	message.PromptTokens = nullableInt(promptTokens)
	message.CompletionTokens = nullableInt(completionTokens)
	message.TotalTokens = nullableInt(totalTokens)
	message.CachedTokens = nullableInt(cachedTokens)
	message.ReasoningTokens = nullableInt(reasoningTokens)
	message.DurationMs = nullableInt(durationMs)
	message.Model = nullableString(model)
	message.ReasoningEffort = nullableString(reasoningEffort)
	var err error
	message.CreatedAt, err = parseSQLiteTime(createdAt)
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
	parsed, err := parseSQLiteTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseSQLiteTime(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}

func defaultJSON(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "[]"
	}
	return json.RawMessage(value)
}
