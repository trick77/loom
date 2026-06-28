package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) CreateProject(ctx context.Context, userID string, in CreateProjectInput) (Project, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	if len(name) > MaxProjectNameLength {
		return Project{}, errors.New("project name is too long")
	}
	description := strings.TrimSpace(in.Description)
	if len(description) > MaxProjectDescriptionLength {
		return Project{}, errors.New("project description is too long")
	}
	// A description the user types at creation time is user-authored, so lock it
	// (description_user_edited = 1) exactly as a manual edit would — otherwise the
	// title-summary auto-generator would overwrite it on the first titled thread.
	userEdited := 0
	if description != "" {
		userEdited = 1
	}
	projectID := newID()

	_, err := s.db.ExecContext(ctx, `
INSERT INTO projects (id, user_id, name, description, description_user_edited, last_activity_at)
VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		projectID, userID, name, description, userEdited,
	)
	if err != nil {
		return Project{}, fmt.Errorf("insert project: %w", err)
	}

	project, ok, err := s.getProject(ctx, userID, projectID)
	if err != nil {
		return Project{}, err
	}
	if !ok {
		return Project{}, errors.New("inserted project not found")
	}
	return project, nil
}

// GetProject returns a single project by id. The bool is false when no such
// project exists for the user.
func (s *Store) GetProject(ctx context.Context, userID, projectID string) (Project, bool, error) {
	return s.getProject(ctx, userID, projectID)
}

func (s *Store) ListProjects(ctx context.Context, userID string, archived bool) ([]Project, error) {
	archiveFilter := "archived_at IS NULL"
	if archived {
		archiveFilter = "archived_at IS NOT NULL"
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, user_id, name, description, starred, archived_at, auto_description_generated_at, description_user_edited, description_source_thread_count, created_at, updated_at, last_activity_at
FROM projects
WHERE user_id = ? AND %s
ORDER BY last_activity_at DESC, id DESC`, archiveFilter),
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return projects, nil
}

func (s *Store) UpdateProject(ctx context.Context, userID, projectID string, in UpdateProjectInput) (Project, bool, error) {
	project, ok, err := s.getProject(ctx, userID, projectID)
	if err != nil || !ok {
		return Project{}, ok, err
	}

	name := project.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" {
			return Project{}, false, errors.New("project name is required")
		}
		if len(name) > MaxProjectNameLength {
			return Project{}, false, errors.New("project name is too long")
		}
	}
	description := project.Description
	descriptionTouched := in.Description != nil
	if descriptionTouched {
		description = strings.TrimSpace(*in.Description)
		if len(description) > MaxProjectDescriptionLength {
			return Project{}, false, errors.New("project description is too long")
		}
	}

	// A user editing the description changes its auto-generation state:
	//   - non-empty → lock it (description_user_edited = 1) so the title-summary
	//     auto-generator never overwrites the hand-written text.
	//   - emptied → re-arm auto-generation: clear the user-edited lock, the debounce
	//     marker, and the source thread count so the very next titled-thread refresh
	//     regenerates immediately with nothing stale blocking it.
	// A name-only edit (in.Description == nil) leaves all three untouched.
	setDescriptionState := ""
	if descriptionTouched {
		if description == "" {
			setDescriptionState = `,
    auto_description_generated_at = NULL,
    description_user_edited = 0,
    description_source_thread_count = 0`
		} else {
			setDescriptionState = `,
    description_user_edited = 1`
		}
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
UPDATE projects
SET name = ?,
    description = ?%s,
    updated_at = datetime('now'),
    last_activity_at = datetime('now')
WHERE user_id = ? AND id = ?`, setDescriptionState),
		name, description, userID, projectID,
	)
	if err != nil {
		return Project{}, false, fmt.Errorf("update project: %w", err)
	}
	return s.getProject(ctx, userID, projectID)
}

// SetAutoProjectDescription stores an auto-generated description (the big-picture
// summary of the project's thread titles) together with the titled-thread count it
// was generated from. The WHERE clause re-checks description_user_edited = 0 under the
// write, so a description the user hand-edited meanwhile is never clobbered (the same
// atomic-guard pattern the one-shot fill used). sourceThreadCount is recorded so the
// refresh gate regenerates only when the titled-thread count next changes. Unlike the
// old SetProjectDescriptionIfEmpty this overwrites an existing auto description — the
// description is now refreshed as the project grows, not filled once.
func (s *Store) SetAutoProjectDescription(ctx context.Context, userID, projectID, generatedDescription string, sourceThreadCount int) (Project, bool, error) {
	description := strings.TrimSpace(generatedDescription)
	if description == "" {
		project, _, err := s.getProject(ctx, userID, projectID)
		return project, false, err
	}
	if runes := []rune(description); len(runes) > MaxProjectDescriptionLength {
		description = strings.TrimSpace(string(runes[:MaxProjectDescriptionLength]))
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE projects
SET description = ?, auto_description_generated_at = datetime('now'), description_source_thread_count = ?
WHERE user_id = ? AND id = ? AND description_user_edited = 0`,
		description, sourceThreadCount, userID, projectID,
	)
	if err != nil {
		return Project{}, false, fmt.Errorf("auto-describe project: %w", err)
	}
	updated, err := changed(result)
	if err != nil {
		return Project{}, false, err
	}
	project, ok, err := s.getProject(ctx, userID, projectID)
	if err != nil || !ok {
		return Project{}, updated, err
	}
	return project, updated, nil
}

func (s *Store) SetProjectStarred(ctx context.Context, userID, projectID string, starred bool) (Project, bool, error) {
	starredInt := 0
	if starred {
		starredInt = 1
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE projects
SET starred = ?
WHERE user_id = ? AND id = ?`,
		starredInt, userID, projectID,
	)
	if err != nil {
		return Project{}, false, fmt.Errorf("star project: %w", err)
	}
	ok, err := changed(result)
	if err != nil || !ok {
		return Project{}, ok, err
	}
	return s.getProject(ctx, userID, projectID)
}

func (s *Store) SetProjectArchived(ctx context.Context, userID, projectID string, archived bool) (bool, error) {
	setArchivedAt := "archived_at = NULL"
	if archived {
		setArchivedAt = "archived_at = datetime('now')"
	}
	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
UPDATE projects
SET %s
WHERE user_id = ? AND id = ?`, setArchivedAt),
		userID, projectID,
	)
	if err != nil {
		return false, fmt.Errorf("archive project: %w", err)
	}
	return changed(result)
}

func (s *Store) DeleteProject(ctx context.Context, userID, projectID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM projects
WHERE user_id = ? AND id = ?`,
		userID, projectID,
	)
	if err != nil {
		return false, fmt.Errorf("delete project: %w", err)
	}
	return changed(result)
}

func (s *Store) getProject(ctx context.Context, userID, projectID string) (Project, bool, error) {
	project, err := scanProject(s.db.QueryRowContext(ctx, `
SELECT id, user_id, name, description, starred, archived_at, auto_description_generated_at, description_user_edited, description_source_thread_count, created_at, updated_at, last_activity_at
FROM projects
WHERE user_id = ? AND id = ?`,
		userID, projectID,
	))
	if err == nil {
		return project, true, nil
	}
	if err == sql.ErrNoRows {
		return Project{}, false, nil
	}
	return Project{}, false, fmt.Errorf("get project: %w", err)
}

func (s *Store) projectExists(ctx context.Context, userID, projectID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `
SELECT 1
FROM projects
WHERE user_id = ? AND id = ?`,
		userID, projectID,
	).Scan(&one)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("check project: %w", err)
}
