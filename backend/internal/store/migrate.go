package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	kitmigrate "github.com/trick77/ship/kit/migrate"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrations is the embedded migrations directory, files at its root as kit's
// runner reads them.
func migrations() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // migrations is embedded at build time; a programmer error
	}
	return sub
}

// renamedMigrations maps the names loom's own runner recorded to the files'
// names now. That runner keyed schema_migrations on a version column and
// ordered by file name, so three numbers were taken twice (0005, 0007, 0013);
// kit's runner orders by the number and refuses a duplicate, so every file
// from the first duplicate on was renumbered, order unchanged.
var renamedMigrations = map[string]string{
	"0005_user_usage.sql":                       "0006_user_usage.sql",
	"0006_artifacts_nullable_thread.sql":        "0007_artifacts_nullable_thread.sql",
	"0007_embedding_usage.sql":                  "0008_embedding_usage.sql",
	"0007_project_auto_description.sql":         "0009_project_auto_description.sql",
	"0008_documents_thread_scope.sql":           "0010_documents_thread_scope.sql",
	"0009_memory_length_caps.sql":               "0011_memory_length_caps.sql",
	"0010_documents_full_text.sql":              "0012_documents_full_text.sql",
	"0011_message_attachments.sql":              "0013_message_attachments.sql",
	"0012_message_content_blocks.sql":           "0014_message_content_blocks.sql",
	"0013_message_context_tokens.sql":           "0015_message_context_tokens.sql",
	"0013_thread_category.sql":                  "0016_thread_category.sql",
	"0014_artifacts_deleted_at.sql":             "0017_artifacts_deleted_at.sql",
	"0015_thread_image_model.sql":               "0018_thread_image_model.sql",
	"0016_artifacts_thumbnail.sql":              "0019_artifacts_thumbnail.sql",
	"0017_shared_threads.sql":                   "0020_shared_threads.sql",
	"0018_project_last_activity.sql":            "0021_project_last_activity.sql",
	"0019_reset_blank_project_descriptions.sql": "0022_reset_blank_project_descriptions.sql",
	"0020_project_description_titles.sql":       "0023_project_description_titles.sql",
	"0021_message_search.sql":                   "0024_message_search.sql",
	"0022_user_directives.sql":                  "0025_user_directives.sql",
	"0023_response_language_drop_auto.sql":      "0026_response_language_drop_auto.sql",
	"0024_message_pasted_texts.sql":             "0027_message_pasted_texts.sql",
	"0025_capitalize_thread_titles.sql":         "0028_capitalize_thread_titles.sql",
	"0026_reset_thread_image_model.sql":         "0029_reset_thread_image_model.sql",
	"0027_cost_nano_usd.sql":                    "0030_cost_nano_usd.sql",
	"0028_shared_threads_user_scoped_fk.sql":    "0031_shared_threads_user_scoped_fk.sql",
	"0029_documents_artifact_fk.sql":            "0032_documents_artifact_fk.sql",
	"0030_threads_recency_index.sql":            "0033_threads_recency_index.sql",
	"0031_artifacts_user_project_index.sql":     "0034_artifacts_user_project_index.sql",
	"0032_vector_model.sql":                     "0035_vector_model.sql",
	"0033_vector_refused.sql":                   "0036_vector_refused.sql",
	"0034_scoped_lookup_indexes.sql":            "0037_scoped_lookup_indexes.sql",
	"0035_usage_code_runs.sql":                  "0038_usage_code_runs.sql",
	"0036_message_client_id.sql":                "0039_message_client_id.sql",
	"0037_queued_messages.sql":                  "0040_queued_messages.sql",
}

// migrate applies the embedded migrations not yet recorded, with kit's runner.
func migrate(db *sql.DB) error {
	return migrateFS(db, migrations())
}

func migrateFS(db *sql.DB, fsys fs.FS) error {
	ctx := context.Background()
	if err := adoptLegacyBookkeeping(ctx, db); err != nil {
		return fmt.Errorf("adopt schema_migrations: %w", err)
	}
	_, err := kitmigrate.Up(ctx, db, fsys, kitmigrate.Options{Dialect: kitmigrate.SQLite})
	return err
}

// adoptLegacyBookkeeping turns loom's own runner's schema_migrations into
// kit's, once: the key column version becomes name, and every renumbered
// file's row takes its new name. One transaction; a database already on
// kit's table, or a new one, is left alone. No new name is another file's old
// one, so the updates never collide.
func adoptLegacyBookkeeping(ctx context.Context, db *sql.DB) error {
	var legacy int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info('schema_migrations') WHERE name = 'version'`).Scan(&legacy); err != nil {
		return err
	}
	if legacy == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `ALTER TABLE schema_migrations RENAME COLUMN version TO name`); err != nil {
		return err
	}
	for old, name := range renamedMigrations {
		if _, err := tx.ExecContext(ctx, `UPDATE schema_migrations SET name = ? WHERE name = ?`, name, old); err != nil {
			return err
		}
	}
	return tx.Commit()
}
