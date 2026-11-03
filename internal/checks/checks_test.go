package checks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzog/drill/internal/config"
	"github.com/enzog/drill/internal/testutil"
)

func expect(t *testing.T, r Result, status Status, msgPart string) {
	t.Helper()
	if r.Status != status || !strings.Contains(r.Message, msgPart) {
		t.Errorf("got %s %q, want %s containing %q", r.Status, r.Message, status, msgPart)
	}
}

func TestMaxAge(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	expect(t, MaxAge(now.Add(-3*time.Hour), now, 26*time.Hour), Pass, "3h0m old (limit 26h)")
	expect(t, MaxAge(now.Add(-30*time.Hour), now, 26*time.Hour), Fail, "30h0m old, older than 26h")
	expect(t, MaxAge(now.Add(-400*24*time.Hour), now, 7*24*time.Hour), Fail, "400d0h old, older than 7d")
	expect(t, MaxAge(now.Add(time.Hour), now, time.Hour), Pass, "0s old")
}

func TestExists(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFiles(t, root, map[string]string{"data/a.txt": "hello"})
	expect(t, Exists(root, "data/a.txt"), Pass, "data/a.txt exists (5 B)")
	expect(t, Exists(root, "data"), Pass, "directory")
	expect(t, Exists(root, "data/missing.txt"), Fail, "missing from the restored snapshot")
}

func TestSQLiteIntegrity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := filepath.Join(root, "data", "db.sqlite3")
	testutil.CreateSQLite(t, db)
	expect(t, SQLiteIntegrity(ctx, root, "data/db.sqlite3"), Pass, "integrity_check ok")

	testutil.CorruptSQLite(t, db)
	r := SQLiteIntegrity(ctx, root, "data/db.sqlite3")
	if r.Status != Fail || !strings.Contains(r.Message, "data/db.sqlite3") {
		t.Errorf("corrupt db: got %s %q", r.Status, r.Message)
	}
	if strings.Contains(r.Message, "\n") || strings.Contains(r.Message, "***") || strings.Count(r.Message, ";") > 3 {
		t.Errorf("corrupt db message should be one short line: %q", r.Message)
	}

	testutil.WriteFiles(t, root, map[string]string{"notdb.sqlite3": strings.Repeat("not a database ", 100)})
	expect(t, SQLiteIntegrity(ctx, root, "notdb.sqlite3"), Fail, "notdb.sqlite3")
	expect(t, SQLiteIntegrity(ctx, root, "nope.sqlite3"), Fail, "missing")
	expect(t, SQLiteIntegrity(ctx, root, "data"), Fail, "not a regular file")
}

func TestSQLiteIntegrityIsReadOnly(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "db.sqlite3")
	testutil.CreateSQLite(t, db)
	before, _ := os.ReadFile(db)
	SQLiteIntegrity(context.Background(), root, "db.sqlite3")
	after, _ := os.ReadFile(db)
	if string(before) != string(after) {
		t.Error("integrity check modified the database")
	}
}

func TestSQLiteDSNWithSpaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dir with spaces")
	testutil.CreateSQLite(t, filepath.Join(root, "db.sqlite3"))
	expect(t, SQLiteIntegrity(context.Background(), root, "db.sqlite3"), Pass, "ok")
}

func TestMinFiles(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFiles(t, root, map[string]string{"a": "1", "b/c": "2", "b/d/e": "3"})
	expect(t, MinFiles(root, 3), Pass, "restored 3 files (minimum 3)")
	expect(t, MinFiles(root, 4), Fail, "restored 3 files, expected at least 4")
	expect(t, MinFiles(filepath.Join(root, "missing"), 1), Fail, "cannot walk")
}

func TestRun(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFiles(t, root, map[string]string{"a.txt": "x"})
	now := time.Now()
	env := Env{Root: root, SnapshotTime: now.Add(-time.Hour), Now: now}
	cases := []struct {
		check  config.Check
		status Status
		name   string
	}{
		{config.Check{Type: config.CheckMaxAge, Duration: 2 * time.Hour}, Pass, "max_age 2h"},
		{config.Check{Type: config.CheckExists, Path: "a.txt"}, Pass, "exists a.txt"},
		{config.Check{Type: config.CheckSQLiteIntegrity, Path: "a.txt"}, Fail, "sqlite_integrity a.txt"},
		{config.Check{Type: config.CheckMinFiles, Count: 1}, Pass, "min_files 1"},
		{config.Check{Type: "bogus"}, Fail, "bogus "},
	}
	for _, tc := range cases {
		r := Run(context.Background(), tc.check, env)
		if r.Status != tc.status || r.Name != tc.name {
			t.Errorf("Run(%+v) = %s %q (%s)", tc.check, r.Status, r.Name, r.Message)
		}
	}
}

func TestFormatAge(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second:               "45s",
		5 * time.Minute:                "5m",
		3*time.Hour + 12*time.Minute:   "3h12m",
		50 * time.Hour:                 "2d2h",
		47*time.Hour + 59*time.Minute:  "47h59m",
		400*24*time.Hour + time.Minute: "400d0h",
	}
	for in, want := range cases {
		if got := FormatAge(in); got != want {
			t.Errorf("FormatAge(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KB", 5 << 20: "5.0 MB"}
	for in, want := range cases {
		if got := FormatSize(in); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSQLiteDSN(t *testing.T) {
	if got := sqliteDSN("/tmp/a b/db"); got != "file:///tmp/a%20b/db?mode=ro" {
		t.Errorf("unix: %q", got)
	}
	if got := sqliteDSN(`C:\x\db`); !strings.HasPrefix(got, "file:///C:/x/db") && !strings.HasPrefix(got, "file:///C:%5Cx%5Cdb") {
		t.Errorf("windows: %q", got)
	}
}

func TestCleanSQLiteErr(t *testing.T) {
	if got := cleanSQLiteErr(errors.New("file is not a database (26)")); got != "file is not a database" {
		t.Errorf("got %q", got)
	}
	if got := cleanSQLiteErr(errors.New("plain")); got != "plain" {
		t.Errorf("got %q", got)
	}
}

func TestJoin(t *testing.T) {
	if got := join("root", "a/b"); got != filepath.Join("root", "a", "b") {
		t.Errorf("got %q", got)
	}
}

func TestPassfFailf(t *testing.T) {
	if r := passf("n=%d", 1); r.Status != Pass || r.Message != "n=1" {
		t.Errorf("passf: %+v", r)
	}
	if r := failf("n=%d", 2); r.Status != Fail || r.Message != "n=2" {
		t.Errorf("failf: %+v", r)
	}
}
