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
	projectID := newID()

	_, err := s.db.ExecContext(ctx, `
INSERT INTO projects (id, user_id, name, description)
VALUES (?, ?, ?, ?)`,
		projectID, userID, name, description,
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
SELECT id, user_id, name, description, starred, archived_at, created_at, updated_at
FROM projects
WHERE user_id = ? AND %s
ORDER BY updated_at DESC, id DESC`, archiveFilter),
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
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
		if len(description) > MaxProjectDescriptionLength {
			return Project{}, false, errors.New("project description is too long")
		}
	}

	_, err = s.db.ExecContext(ctx, `
UPDATE projects
SET name = ?, description = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		name, description, userID, projectID,
	)
	if err != nil {
		return Project{}, false, fmt.Errorf("update project: %w", err)
	}
	return s.getProject(ctx, userID, projectID)
}

func (s *Store) SetProjectStarred(ctx context.Context, userID, projectID string, starred bool) (Project, bool, error) {
	starredInt := 0
	if starred {
		starredInt = 1
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE projects
SET starred = ?, updated_at = datetime('now')
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
SET %s, updated_at = datetime('now')
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
SELECT id, user_id, name, description, starred, archived_at, created_at, updated_at
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
