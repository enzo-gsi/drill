package main

// End-to-end tests proving drill catches real backup problems. Each one
// builds a throwaway restic repository and runs the CLI against it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enzo-gsi/drill/internal/testutil"
)

// isolateTemp points the temp dir at a fresh directory so tests can verify
// that drill cleans up its restore dirs.
func isolateTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, dir)
	}
	return dir
}

func assertNoRestoreDirs(t *testing.T, tmp string) {
	t.Helper()
	entries, _ := os.ReadDir(tmp)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "drill-") {
			t.Errorf("restore dir %s was not deleted", e.Name())
		}
	}
}

func expectFailure(t *testing.T, r result, wantCode int, wants ...string) {
	t.Helper()
	if r.code != wantCode {
		t.Fatalf("exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, wantCode, r.stdout, r.stderr)
	}
	out := r.stdout + r.stderr
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, testutil.Password) {
		t.Error("output leaks the repository password")
	}
}

func TestFailureCorruptedSQLite(t *testing.T) {
	tmp := isolateTemp(t)
	f := newFixture(t, allChecks)
	testutil.CorruptSQLite(t, filepath.Join(f.src, "data", "db.sqlite3"))
	f.repo.Backup(t, f.src, "--tag", "app")

	r := drill(t, "run", "--config", f.config)
	expectFailure(t, r, exitFailed,
		"PASS  restore",
		"FAIL  sqlite_integrity data/db.sqlite3",
		"app: FAIL, 1 of 6 checks failed")
	assertNoRestoreDirs(t, tmp)
}

func TestFailureMissingFile(t *testing.T) {
	tmp := isolateTemp(t)
	f := newFixture(t, allChecks)
	os.Remove(filepath.Join(f.src, "data", "notes.txt"))
	f.repo.Backup(t, f.src, "--tag", "app")

	r := drill(t, "run", "--config", f.config)
	expectFailure(t, r, exitFailed,
		"FAIL  exists data/notes.txt",
		"data/notes.txt is missing from the restored snapshot",
		"FAIL  min_files 4", "restored 3 files, expected at least 4")
	assertNoRestoreDirs(t, tmp)
}

func TestFailureOldSnapshot(t *testing.T) {
	f := newFixture(t, allChecks)
	f.repo.Backup(t, f.src, "--tag", "app", "--time", "2024-01-01 00:00:00")

	r := drill(t, "run", "--config", f.config)
	expectFailure(t, r, exitFailed, "FAIL  max_age 26h", "older than 26h", "PASS  restore")
}

func TestFailureOnlyOtherTagIsRecent(t *testing.T) {
	f := newFixture(t, allChecks)
	f.repo.Backup(t, f.src, "--tag", "app", "--time", "2024-01-01 00:00:00")
	f.repo.Backup(t, f.src, "--tag", "other")

	r := drill(t, "run", "--config", f.config)
	expectFailure(t, r, exitFailed, "FAIL  max_age 26h")
}

func TestFailureNoSnapshots(t *testing.T) {
	f := newFixture(t, allChecks)
	r := drill(t, "run", "--config", f.config)
	expectFailure(t, r, exitFailed, "FAIL  snapshot", `no snapshots found`, `with tag "app"`, "SKIP  exists data/notes.txt")
}

func TestFailureWrongPassword(t *testing.T) {
	tmp := isolateTemp(t)
	f := newFixture(t, allChecks)
	f.repo.Backup(t, f.src, "--tag", "app")
	wrong := filepath.Join(t.TempDir(), "wrong-pass")
	os.WriteFile(wrong, []byte("hunter2\n"), 0o600)
	f.writeConfig(t, wrong, allChecks)

	r := drill(t, "run", "--config", f.config)
	expectFailure(t, r, exitFailed, "FAIL  snapshot", "wrong password or no key found", "SKIP  restore")
	if strings.Contains(r.stdout+r.stderr, "hunter2") {
		t.Error("output leaks the wrong password")
	}
	r = drill(t, "run", "--json", "--config", f.config)
	expectFailure(t, r, exitFailed, `"status": "fail"`, `"snapshot": null`, "wrong password")
	if strings.Contains(r.stdout, "hunter2") {
		t.Error("JSON leaks the wrong password")
	}
	assertNoRestoreDirs(t, tmp)
}

func TestFailureMissingRepo(t *testing.T) {
	f := newFixture(t, allChecks)
	f.repo.Path = filepath.Join(t.TempDir(), "gone")
	f.writeConfig(t, f.repo.PasswordFile, allChecks)

	r := drill(t, "run", "--config", f.config)
	expectFailure(t, r, exitFailed, "FAIL  snapshot", "repository does not exist at")
	if _, err := os.Stat(f.repo.Path); !os.IsNotExist(err) {
		t.Error("drill created the missing repository")
	}
}

func TestFailureConfigErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "drill.yml")
	os.WriteFile(p, []byte("targets:\n  - name: app\n    backend: borg\n    repo: /r\n    password_file: /nope\n    checks:\n      - max_age: tomorrow\n      - checksum: x\n"), 0o644)
	r := drill(t, "run", "--config", p)
	expectFailure(t, r, exitUsage,
		"invalid config",
		`unknown backend "borg" (supported: restic)`,
		"password_file",
		`bad duration "tomorrow"`,
		`unknown check "checksum"`)
}

func TestFailureDoesNotModifyRepo(t *testing.T) {
	f := newFixture(t, allChecks)
	f.repo.Backup(t, f.src, "--tag", "app")
	before := snapshotTree(t, f.repo.Path)
	drill(t, "run", "--config", f.config)
	after := snapshotTree(t, f.repo.Path)
	if before != after {
		t.Errorf("repository changed during drill:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// snapshotTree lists every file in dir with its size.
func snapshotTree(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			b.WriteString(rel + " " + info.ModTime().String() + "\n")
		}
		return nil
	})
	return b.String()
}
