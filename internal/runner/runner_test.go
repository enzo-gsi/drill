package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzog/drill/internal/backend"
	"github.com/enzog/drill/internal/backend/restic"
	"github.com/enzog/drill/internal/checks"
	"github.com/enzog/drill/internal/config"
	"github.com/enzog/drill/internal/report"
)

type fakeBackend struct {
	snap       backend.Snapshot
	snapErr    error
	restoreErr error
	files      map[string]string
	restoredTo string
}

func (f *fakeBackend) LatestSnapshot(context.Context) (backend.Snapshot, error) {
	return f.snap, f.snapErr
}

func (f *fakeBackend) Restore(_ context.Context, _ backend.Snapshot, dest string) error {
	f.restoredTo = dest
	for name, content := range f.files {
		p := filepath.Join(dest, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o444) // read-only, like some restores
	}
	return f.restoreErr
}

var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func newRunner(t *testing.T, fb *fakeBackend) Runner {
	return Runner{
		NewBackend: func(config.Target) backend.Backend { return fb },
		Now:        func() time.Time { return now },
		TempDir:    t.TempDir(),
	}
}

var target = config.Target{Name: "app", Checks: []config.Check{
	{Type: config.CheckMaxAge, Duration: 26 * time.Hour},
	{Type: config.CheckExists, Path: "data/a.txt"},
	{Type: config.CheckMinFiles, Count: 1},
}}

func statuses(cs []report.Check) []checks.Status {
	out := make([]checks.Status, len(cs))
	for i, c := range cs {
		out[i] = c.Status
	}
	return out
}

func equal(a, b []checks.Status) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRunTargetPass(t *testing.T) {
	fb := &fakeBackend{snap: backend.Snapshot{ID: "id1", ShortID: "id", Time: now.Add(-time.Hour)}, files: map[string]string{"data/a.txt": "x"}}
	r := newRunner(t, fb)
	got := r.RunTarget(context.Background(), target)
	if got.Status != checks.Pass {
		t.Fatalf("expected pass, got %+v", got)
	}
	want := []checks.Status{checks.Pass, checks.Pass, checks.Pass, checks.Pass, checks.Pass}
	if !equal(statuses(got.Checks), want) || got.Checks[0].Name != CheckSnapshot || got.Checks[1].Name != CheckRestore {
		t.Errorf("checks: %+v", got.Checks)
	}
	if got.Snapshot == nil || got.Snapshot.ShortID != "id" {
		t.Errorf("snapshot: %+v", got.Snapshot)
	}
	if _, err := os.Stat(fb.restoredTo); !os.IsNotExist(err) {
		t.Errorf("temp dir %s was not deleted", fb.restoredTo)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("warnings: %v", got.Warnings)
	}
}

func TestRunTargetSnapshotError(t *testing.T) {
	fb := &fakeBackend{snapErr: &restic.Error{Op: "snapshots", Code: 12, Msg: "wrong password or no key found"}}
	got := newRunner(t, fb).RunTarget(context.Background(), target)
	want := []checks.Status{checks.Fail, checks.Skip, checks.Skip, checks.Skip, checks.Skip}
	if got.Status != checks.Fail || !equal(statuses(got.Checks), want) || got.Snapshot != nil || got.Checks[1].Name != CheckRestore {
		t.Fatalf("got %+v", got)
	}
	if fb.restoredTo != "" {
		t.Error("must not restore without a snapshot")
	}
}

func TestRunTargetRestoreError(t *testing.T) {
	fb := &fakeBackend{snap: backend.Snapshot{ID: "id1", Time: now.Add(-48 * time.Hour)}, restoreErr: errors.New("boom"), files: map[string]string{"junk": "x"}}
	got := newRunner(t, fb).RunTarget(context.Background(), target)
	// snapshot ok, restore fails, max_age still runs (and fails: 48h > 26h), others skipped.
	want := []checks.Status{checks.Pass, checks.Fail, checks.Fail, checks.Skip, checks.Skip}
	if got.Status != checks.Fail || !equal(statuses(got.Checks), want) {
		t.Fatalf("got %+v", got.Checks)
	}
	if _, err := os.Stat(fb.restoredTo); !os.IsNotExist(err) {
		t.Error("temp dir must be deleted even when restore fails")
	}
}

func TestRunTargetCheckFails(t *testing.T) {
	fb := &fakeBackend{snap: backend.Snapshot{ID: "id1", Time: now}, files: map[string]string{"other.txt": "x"}}
	got := newRunner(t, fb).RunTarget(context.Background(), target)
	want := []checks.Status{checks.Pass, checks.Pass, checks.Pass, checks.Fail, checks.Pass}
	if got.Status != checks.Fail || !equal(statuses(got.Checks), want) {
		t.Fatalf("got %+v", got.Checks)
	}
}

func TestRunTargetTempDirError(t *testing.T) {
	fb := &fakeBackend{snap: backend.Snapshot{ID: "id1", Time: now}}
	r := newRunner(t, fb)
	r.TempDir = filepath.Join(t.TempDir(), "does", "not", "exist")
	got := r.RunTarget(context.Background(), target)
	if got.Status != checks.Fail || got.Checks[1].Name != CheckRestore || got.Checks[1].Status != checks.Fail {
		t.Fatalf("got %+v", got.Checks)
	}
}

func TestRun(t *testing.T) {
	fb := &fakeBackend{snap: backend.Snapshot{ID: "id1", Time: now}, files: map[string]string{"data/a.txt": "x"}}
	bad := config.Target{Name: "bad", Checks: []config.Check{{Type: config.CheckExists, Path: "missing"}}}
	rep := newRunner(t, fb).Run(context.Background(), []config.Target{target, bad})
	if len(rep.Targets) != 2 || rep.Status != checks.Fail || rep.Targets[0].Status != checks.Pass {
		t.Fatalf("got %+v", rep)
	}
}

func TestNewBackend(t *testing.T) {
	b := NewBackend(config.Target{Repo: "/r", PasswordFile: "/p", Tag: "t"})
	r, ok := b.(*restic.Restic)
	if !ok || r.Repo != "/r" || r.PasswordFile != "/p" || r.Tag != "t" {
		t.Errorf("got %#v", b)
	}
}

func TestNow(t *testing.T) {
	if (Runner{}).now().IsZero() {
		t.Error("default now should be the real time")
	}
}

func TestSkipped(t *testing.T) {
	got := skipped(target.Checks, "why")
	if len(got) != 3 || got[0].Status != checks.Skip || got[0].Message != "not run: why" || got[1].Name != "exists data/a.txt" {
		t.Errorf("got %+v", got)
	}
}

func TestImplicit(t *testing.T) {
	c := implicit("restore", checks.Pass, "ok", time.Now())
	if c.Name != "restore" || c.Status != checks.Pass || c.Message != "ok" {
		t.Errorf("got %+v", c)
	}
}

func TestRemoveAllReadOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("x"), 0o444)
	os.Chmod(filepath.Join(dir, "sub"), 0o555)
	if err := removeAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("dir still exists")
	}
}
