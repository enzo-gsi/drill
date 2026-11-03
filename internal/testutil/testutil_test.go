package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	WriteFiles(t, dir, map[string]string{"a/b/c.txt": "hello"})
	got, err := os.ReadFile(filepath.Join(dir, "a", "b", "c.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRepoBackup(t *testing.T) {
	r := NewRepo(t)
	src := t.TempDir()
	WriteFiles(t, src, map[string]string{"x.txt": "x"})
	r.Backup(t, src, "--tag", "t")
	out := r.Restic(t, "snapshots", "--json")
	if !strings.Contains(out, `"t"`) {
		t.Fatalf("snapshot with tag not found: %s", out)
	}
}
