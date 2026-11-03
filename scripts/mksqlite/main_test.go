package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enzo-gsi/drill/internal/checks"
)

func TestCreateAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "db.sqlite3")
	if err := createDB(p); err != nil {
		t.Fatal(err)
	}
	if r := checks.SQLiteIntegrity(context.Background(), dir, "db.sqlite3"); r.Status != checks.Pass {
		t.Fatalf("fresh db should pass: %s", r.Message)
	}
	if err := corruptDB(p); err != nil {
		t.Fatal(err)
	}
	if r := checks.SQLiteIntegrity(context.Background(), dir, "db.sqlite3"); r.Status != checks.Fail {
		t.Fatalf("corrupted db should fail: %s", r.Message)
	}
}

func TestCorruptTooSmall(t *testing.T) {
	p := filepath.Join(t.TempDir(), "small")
	os.WriteFile(p, []byte("tiny"), 0o644)
	if err := corruptDB(p); err == nil {
		t.Error("expected error for a tiny file")
	}
	if err := corruptDB(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected error for a missing file")
	}
}
