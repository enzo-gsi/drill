// Package testutil builds throwaway restic repositories for tests.
// Everything it creates lives under t.TempDir().
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Password is the password of every repository created by NewRepo.
const Password = "drill-test-password"

// Repo is a throwaway restic repository.
type Repo struct {
	Path         string
	PasswordFile string
}

// RequireRestic skips the test when restic is not installed.
func RequireRestic(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
}

// NewRepo initialises an empty restic repository in a temp dir.
func NewRepo(t *testing.T) Repo {
	t.Helper()
	RequireRestic(t)
	dir := t.TempDir()
	r := Repo{Path: filepath.Join(dir, "repo"), PasswordFile: filepath.Join(dir, "pass")}
	if err := os.WriteFile(r.PasswordFile, []byte(Password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.Restic(t, "init")
	return r
}

// Backup backs up dir with extra restic backup flags (e.g. --tag, --time).
func (r Repo) Backup(t *testing.T, dir string, flags ...string) {
	t.Helper()
	r.Restic(t, append(append([]string{"backup"}, flags...), dir)...)
}

// Restic runs a restic command against the repository and fails the test on error.
func (r Repo) Restic(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{"--repo", r.Path, "--password-file", r.PasswordFile, "--quiet"}, args...)
	out, err := exec.Command("restic", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("restic %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// WriteFiles creates files (slash-separated relative path -> content) under dir.
func WriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
