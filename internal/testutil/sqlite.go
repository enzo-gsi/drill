package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // pure Go SQLite driver
)

// CreateSQLite writes a valid SQLite database with enough rows to span many pages.
func CreateSQLite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		"PRAGMA journal_mode=DELETE",
		"CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE INDEX items_name ON items(name)",
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	pad := strings.Repeat("x", 200)
	for i := 0; i < 500; i++ {
		if _, err := tx.Exec("INSERT INTO items (name) VALUES (?)", pad+string(rune('a'+i%26))); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// CorruptSQLite overwrites the middle of a database file with garbage while
// keeping its header, so SQLite still opens it but integrity_check fails.
func CorruptSQLite(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8192 {
		t.Fatalf("%s is too small to corrupt (%d bytes)", path, len(data))
	}
	for i := len(data) / 2; i < len(data)/2+4096 && i < len(data); i++ {
		data[i] = 0xA5
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
