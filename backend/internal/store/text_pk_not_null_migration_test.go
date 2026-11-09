package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// textKeyTables are the tables 0054 rebuilds and their TEXT primary keys,
// which SQLite let hold NULL.
var textKeyTables = map[string]string{
	"artifacts":           "id",
	"documents":           "id",
	"messages":            "id",
	"notifications":       "id",
	"projects":            "id",
	"push_subscriptions":  "id",
	"queued_messages":     "id",
	"schedule_runs":       "id",
	"schedules":           "id",
	"settings":            "key",
	"shared_threads":      "id",
	"threads":             "id",
	"user_fact_cursor":    "user_id",
	"user_facts":          "id",
	"user_location_state": "user_id",
	"user_usage_totals":   "user_id",
	"users":               "id",
}

// 0054 rebuilds 17 tables with NOT NULL keys and 0055 restores messages'
// search triggers. A database at 0053 with rows in every table (and a NULL key
// in three) is migrated: every row, rowid, index, trigger and foreign key
// stays, only the NULL-key rows go, search still finds old and new messages,
// and a NULL key is now refused.
func TestMigration0054_textPrimaryKeysNotNull(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0053_drop_user_today_seen.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	// Gapped rowids: a copy that renumbered rows would show.
	for _, q := range []string{
		`INSERT INTO users (rowid, id, oidc_subject, username, role, timezone) VALUES (5, 'u', 's', 'u', 'admin', 'Europe/Zurich')`,
		`INSERT INTO users (rowid, id, oidc_subject, username, role) VALUES (9, 'v', 's2', 'v', 'user')`,
		`INSERT INTO projects (rowid, id, user_id, name, starred) VALUES (4, 'p', 'u', 'P', 1)`,
		`INSERT INTO schedules (rowid, id, user_id, name, prompt, interval_text, cron_template, cron, timezone)
		 VALUES (3, 'sc', 'u', 'S', 'news', 'daily', '~ 8 * * *', '7 8 * * *', 'UTC')`,
		`INSERT INTO threads (rowid, id, user_id, project_id, title, schedule_id) VALUES (6, 't', 'u', 'p', 'T', 'sc')`,
		`INSERT INTO messages (rowid, id, thread_id, user_id, role, content) VALUES (5, 'm1', 't', 'u', 'user', 'platypus question')`,
		`INSERT INTO messages (rowid, id, thread_id, user_id, role, content) VALUES (7, NULL, 't', 'u', 'user', 'orphan wombat')`,
		`INSERT INTO messages (rowid, id, thread_id, user_id, role, content, client_message_id) VALUES (9, 'm2', 't', 'u', 'assistant', 'platypus answer', 'c0')`,
		`INSERT INTO queued_messages (rowid, id, thread_id, user_id, content, client_message_id) VALUES (8, 'q', 't', 'u', 'later', 'c1')`,
		`INSERT INTO shared_threads (rowid, id, share_id, thread_id, user_id, title, snapshot) VALUES (2, 'sh', 'pub', 't', 'u', 'T', '{}')`,
		`INSERT INTO artifacts (rowid, id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes)
		 VALUES (3, 'a', 'u', 't', 'p', 'f.txt', 'u/f.txt', 'text/plain', 4)`,
		`INSERT INTO documents (rowid, id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes, status)
		 VALUES (4, 'd', 'u', 'p', 'a', 'u/f.txt', 'f.txt', 'text/plain', 4, 'embedded')`,
		`INSERT INTO chunks (id, document_id, user_id, project_id, ordinal, text) VALUES (11, 'd', 'u', 'p', 0, 'text')`,
		`INSERT INTO vector_refused (chunk_id) VALUES (11)`,
		`INSERT INTO project_memory (user_id, project_id, content) VALUES ('u', 'p', 'remember')`,
		`INSERT INTO schedule_runs (rowid, id, schedule_id, user_id, message_id, thread_id, status) VALUES (6, 'r', 'sc', 'u', 'm2', 't', 'new')`,
		`INSERT INTO schedule_queries (schedule_id, user_id, query) VALUES ('sc', 'u', 'news')`,
		`INSERT INTO schedule_sources (schedule_id, user_id, url_key) VALUES ('sc', 'u', 'example.com')`,
		`INSERT INTO notifications (rowid, id, user_id, kind, schedule_id, thread_id, message_id, title)
		 VALUES (5, 'n', 'u', 'schedule_run', 'sc', 't', 'm2', 'New')`,
		`INSERT INTO push_subscriptions (rowid, id, user_id, endpoint, p256dh, auth, vapid_key) VALUES (7, 'ps', 'u', 'https://push', 'k', 'a', 'v')`,
		`INSERT INTO settings (rowid, key, value) VALUES (3, 'site', 'x')`,
		`INSERT INTO user_facts (rowid, id, user_id, category, content, origin, first_seen, last_seen)
		 VALUES (4, 'f', 'u', 'interest', 'likes tea', 'extracted', '2026-01-01', '2026-01-02')`,
		`INSERT INTO user_fact_sources (fact_id, user_id, thread_id) VALUES ('f', 'u', 't')`,
		`INSERT INTO user_fact_cursor (rowid, user_id, last_message_id) VALUES (3, 'u', 'm2')`,
		`INSERT INTO user_fact_cursor (rowid, user_id, last_message_id) VALUES (8, NULL, 'x')`,
		`INSERT INTO user_location_state (rowid, user_id, facts_hash) VALUES (2, 'u', 'h')`,
		`INSERT INTO user_locations (user_id, kind, name, latitude, longitude) VALUES ('u', 'home', 'Bern', 46.9, 7.4)`,
		`INSERT INTO user_usage_totals (rowid, user_id, total_tokens, code_runs) VALUES (4, 'u', 7, 2)`,
		`INSERT INTO user_usage_totals (rowid, user_id, total_tokens) VALUES (6, NULL, 1)`,
		`INSERT INTO user_usage_totals (rowid, user_id, total_tokens) VALUES (9, NULL, 1)`,
		`INSERT INTO user_directives (id, user_id, content) VALUES ('dir', 'u', 'be brief')`,
	} {
		exec(q)
	}

	before := countRows(t, db)
	rowsBefore := keyedRows(t, db)
	schemaBefore := schemaObjects(t, db)
	colsBefore := tableShapes(t, db)
	sqlBefore := tableSQL(t, db)

	if err := migrate(db); err != nil {
		t.Fatalf("apply 0054 and 0055: %v", err)
	}

	// Only the NULL-key rows went: one message (and its search row), one
	// cursor, two usage rows.
	want := before
	want["messages"]--
	want["message_fts"]--
	want["user_fact_cursor"]--
	want["user_usage_totals"] -= 2
	if got := countRows(t, db); !reflect.DeepEqual(got, want) {
		t.Fatalf("row counts after rebuild = %v, want %v", got, want)
	}
	for table, rows := range rowsBefore {
		kept := rows[:0:0]
		for _, r := range rows {
			if !strings.HasSuffix(r, "=<nil>") {
				kept = append(kept, r)
			}
		}
		rowsBefore[table] = kept
	}
	if got := keyedRows(t, db); !reflect.DeepEqual(got, rowsBefore) {
		t.Fatalf("rows after rebuild = %v, want %v (rowid and key kept)", got, rowsBefore)
	}
	if got := schemaObjects(t, db); !reflect.DeepEqual(got, schemaBefore) {
		t.Fatalf("indexes and triggers after rebuild = %v\nwant %v", got, schemaBefore)
	}
	for table, key := range textKeyTables {
		prefix := "0 " + key + " notnull=0 "
		if !strings.HasPrefix(colsBefore[table], prefix) {
			t.Fatalf("%s: key %s is not a nullable first column before the rebuild", table, key)
		}
		colsBefore[table] = "0 " + key + " notnull=1 " + strings.TrimPrefix(colsBefore[table], prefix)
	}
	if got := tableShapes(t, db); !reflect.DeepEqual(got, colsBefore) {
		t.Fatalf("columns and foreign keys after rebuild = %v\nwant %v", got, colsBefore)
	}
	sqlAfter := tableSQL(t, db)
	for table, key := range textKeyTables {
		old := "(" + key + "TEXTPRIMARYKEY"
		if !strings.Contains(sqlBefore[table], old) {
			t.Fatalf("%s: %q not in %s", table, old, sqlBefore[table])
		}
		want := strings.Replace(sqlBefore[table], old, "("+key+"TEXTNOTNULLPRIMARYKEY", 1)
		if sqlAfter[table] != want {
			t.Fatalf("%s after rebuild:\n%s\nwant\n%s", table, sqlAfter[table], want)
		}
	}

	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q (err %v)", integrity, err)
	}
	if v := queryStrings(t, db, `SELECT "table" || ' -> ' || parent FROM pragma_foreign_key_check`); len(v) != 0 {
		t.Fatalf("foreign_key_check reports violations: %v", v)
	}

	// Search still points at the right messages, and the restored triggers
	// index a new one.
	exec(`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES ('m3', 't', 'u', 'user', 'platypus follow-up')`)
	if got := ftsMatch(t, db, "platypus"); !reflect.DeepEqual(got, []string{"m1", "m2", "m3"}) {
		t.Fatalf("search for platypus = %v, want m1 m2 m3", got)
	}
	if got := ftsMatch(t, db, "wombat"); len(got) != 0 {
		t.Fatalf("search for the deleted NULL-key message = %v, want none", got)
	}
	exec(`UPDATE messages SET content = 'echidna now' WHERE id = 'm1'`)
	exec(`DELETE FROM messages WHERE id = 'm3'`)
	if got := ftsMatch(t, db, "platypus"); !reflect.DeepEqual(got, []string{"m2"}) {
		t.Fatalf("search after update and delete = %v, want m2", got)
	}

	for _, q := range []string{
		`INSERT INTO user_usage_totals (user_id, total_tokens) VALUES (NULL, 1)`,
		`INSERT INTO user_fact_cursor (user_id) VALUES (NULL)`,
		`INSERT INTO threads (id, user_id, title) VALUES (NULL, 'u', 'T')`,
		`INSERT INTO shared_threads (id, share_id, thread_id, user_id, title, snapshot) VALUES (NULL, 'pub2', 't', 'u', 'T', '{}')`,
		`INSERT INTO settings (key, value) VALUES (NULL, 'x')`,
	} {
		_, err := db.Exec(q)
		if err == nil || !strings.Contains(err.Error(), "NOT NULL constraint failed") {
			t.Fatalf("%s: err = %v, want a NOT NULL failure", q, err)
		}
	}
	for table, key := range textKeyTables {
		var notNull int
		if err := db.QueryRow(`SELECT "notnull" FROM pragma_table_info(?) WHERE name = ?`, table, key).Scan(&notNull); err != nil || notNull != 1 {
			t.Fatalf("%s.%s notnull = %d (err %v), want 1", table, key, notNull, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO user_directives (id, user_id, content) VALUES ('x', 'missing', 'x')`); err == nil {
		t.Fatal("directive for a missing user inserted: foreign keys were left off")
	}
	// No table keeps a nullable TEXT key, listed above or not (schema_migrations
	// is kit's runner's).
	if v := queryStrings(t, db, `SELECT m.name || '.' || p.name FROM sqlite_master m, pragma_table_info(m.name) p
		WHERE m.type = 'table' AND m.sql NOT LIKE 'CREATE VIRTUAL%' AND m.name NOT LIKE 'message_fts_%'
		AND m.name NOT LIKE 'vec_chunks_%' AND m.name <> 'schema_migrations' AND p.pk > 0 AND p."notnull" = 0 AND upper(p.type) = 'TEXT'`); len(v) != 0 {
		t.Fatalf("nullable TEXT keys left: %v", v)
	}
}

// A 0.10.0 database (at 0052) with a user's today_seen_on set reaches the end
// in one run: 0053 drops the column, then 0054 rebuilds users without it.
func TestMigration0054_fromUserTodaySeen(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0052_user_today_seen.sql")
	if _, err := db.Exec(`INSERT INTO users (rowid, id, oidc_subject, username, role, today_seen_on)
		VALUES (5, 'u', 's', 'u', 'admin', '2026-11-08')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if got := queryStrings(t, db, `SELECT rowid || ':' || id || ':' || username FROM users`); !reflect.DeepEqual(got, []string{"5:u:u"}) {
		t.Fatalf("users after migrate = %v, want 5:u:u", got)
	}
	if got := queryStrings(t, db, `SELECT name FROM pragma_table_info('users') WHERE name = 'today_seen_on'`); len(got) != 0 {
		t.Fatalf("today_seen_on still there: %v", got)
	}
}

// countRows counts every ordinary table and message_fts; virtual-table
// shadow tables are left out, their layout is the module's.
func countRows(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	tables := queryStrings(t, db, `SELECT name FROM sqlite_master WHERE type = 'table'
		AND sql NOT LIKE 'CREATE VIRTUAL%' AND name NOT LIKE 'message_fts_%' AND name NOT LIKE 'vec_chunks_%'
		AND name NOT IN ('sqlite_sequence', 'schema_migrations')`)
	tables = append(tables, "message_fts")
	out := map[string]int{}
	for _, table := range tables {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM "` + table + `"`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

// keyedRows is every rebuilt table's rows as "rowid:key", in rowid order.
func keyedRows(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for table, key := range textKeyTables {
		rows, err := db.Query(`SELECT rowid, ` + key + ` FROM ` + table + ` ORDER BY rowid`)
		if err != nil {
			t.Fatalf("rows of %s: %v", table, err)
		}
		for rows.Next() {
			var rowid int64
			var k sql.NullString
			if err := rows.Scan(&rowid, &k); err != nil {
				t.Fatal(err)
			}
			v := "<nil>"
			if k.Valid {
				v = k.String
			}
			out[table] = append(out[table], fmt.Sprintf("%d=%s", rowid, v))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows of %s: %v", table, err)
		}
		_ = rows.Close()
	}
	return out
}

// tableSQL is each rebuilt table's CREATE TABLE text without whitespace and
// double quotes (a rename quotes the name), so CHECKs, defaults and
// constraints compare exactly while the layout may differ.
func tableSQL(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for table := range textKeyTables {
		var s string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&s); err != nil {
			t.Fatalf("sql of %s: %v", table, err)
		}
		out[table] = strings.Join(strings.Fields(strings.ReplaceAll(s, `"`, "")), "")
	}
	return out
}

// schemaObjects is every index and trigger, by name, with its SQL (empty for
// the automatic indexes of PRIMARY KEY and UNIQUE).
func schemaObjects(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return queryStrings(t, db, `SELECT type || ' ' || name || ' ON ' || tbl_name || ': ' || coalesce(sql, '')
		FROM sqlite_master WHERE type IN ('index', 'trigger') ORDER BY type, name`)
}

// tableShapes is each rebuilt table's columns (order, type, NOT NULL,
// default, key position), foreign keys and the columns of every index.
func tableShapes(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for table := range textKeyTables {
		cols := queryStrings(t, db, `SELECT cid || ' ' || name || ' notnull=' || "notnull" || ' ' || type
			|| ' default=' || coalesce(dflt_value, '-') || ' pk=' || pk FROM pragma_table_info('`+table+`') ORDER BY cid`)
		fks := queryStrings(t, db, `SELECT "table" || '(' || coalesce("to", '') || ') from ' || "from" || ' seq ' || seq
			|| ' ' || on_update || ' ' || on_delete FROM pragma_foreign_key_list('`+table+`') ORDER BY "table", "from", seq`)
		idx := queryStrings(t, db, `SELECT il.name || ' unique=' || il."unique" || ' ' || il.origin || ' partial=' || il.partial
			|| ' ' || (SELECT group_concat(coalesce(x.name, '<expr>') || ':' || x."desc", ',') FROM pragma_index_xinfo(il.name) x WHERE x.key)
			FROM pragma_index_list('`+table+`') il ORDER BY il.name`)
		out[table] = strings.Join(cols, "\n") + "\n" + strings.Join(fks, "\n") + "\n" + strings.Join(idx, "\n")
	}
	return out
}

func ftsMatch(t *testing.T, db *sql.DB, term string) []string {
	t.Helper()
	return queryStrings(t, db, `SELECT m.id FROM message_fts f JOIN messages m ON m.rowid = f.rowid
		WHERE message_fts MATCH '`+term+`' AND f.message_id = m.id ORDER BY m.id`)
}

func queryStrings(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
