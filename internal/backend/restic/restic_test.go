package restic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzog/drill/internal/backend"
	"github.com/enzog/drill/internal/testutil"
)

func TestParseSnapshots(t *testing.T) {
	data := `[{"time":"2026-11-03T17:52:29.3057936+01:00","paths":["/data"],"hostname":"h","tags":["vw"],"id":"aaaa","short_id":"aa"},
	{"time":"2026-11-04T01:00:00Z","paths":["/data"],"hostname":"h","id":"bbbb","short_id":"bb"}]`
	snaps, err := parseSnapshots([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 || snaps[0].ShortID != "aa" || snaps[0].Tags[0] != "vw" || snaps[1].Paths[0] != "/data" {
		t.Fatalf("unexpected snapshots: %+v", snaps)
	}
	if _, err := parseSnapshots([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
	if _, err := parseSnapshots([]byte(`[{"time":"yesterday","id":"x"}]`)); err == nil {
		t.Error("expected error for invalid time")
	}
}

func TestParseTime(t *testing.T) {
	got, err := parseTime("2026-11-03T17:52:29.3057936+01:00")
	if err != nil || got.UTC().Hour() != 16 {
		t.Errorf("parseTime: %v %v", got, err)
	}
}

func TestNewest(t *testing.T) {
	now := time.Now()
	snaps := []backend.Snapshot{{ID: "old", Time: now.Add(-time.Hour)}, {ID: "new", Time: now}, {ID: "mid", Time: now.Add(-time.Minute)}}
	if s, ok := newest(snaps); !ok || s.ID != "new" {
		t.Errorf("newest = %v %v", s.ID, ok)
	}
	if _, ok := newest(nil); ok {
		t.Error("newest(nil) should report false")
	}
}

func TestTreePath(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\data`: "/C/Users/me/data",
		"d:":               "/d",
		"/home/me/data":    "/home/me/data",
		"relative":         "/relative",
	}
	for in, want := range cases {
		if got := treePath(in); got != want {
			t.Errorf("treePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsLetter(t *testing.T) {
	if !isLetter('C') || !isLetter('z') || isLetter('1') || isLetter('/') {
		t.Error("isLetter misclassified")
	}
}

func TestRestoreSpec(t *testing.T) {
	if got := restoreSpec(backend.Snapshot{ID: "abc", Paths: []string{"/srv/data"}}); got != "abc:/srv/data" {
		t.Errorf("single path: %q", got)
	}
	if got := restoreSpec(backend.Snapshot{ID: "abc", Paths: []string{"/a", "/b"}}); got != "abc" {
		t.Errorf("multiple paths: %q", got)
	}
}

func TestErrorMessage(t *testing.T) {
	jsonErr := `{"message_type":"exit_error","code":1,"message":"Fatal: something broke\nmore detail"}`
	cases := []struct {
		stderr string
		code   int
		want   string
	}{
		{"", 10, "repository does not exist at /r"},
		{"", 12, "wrong password or no key found"},
		{jsonErr, 1, "something broke"},
		{"warning\nFatal: plain text error\n", 1, "plain text error"},
		{"", 1, "failed without an error message"},
		{strings.Repeat("x", 400), 1, strings.Repeat("x", 300) + "..."},
	}
	for _, tc := range cases {
		if got := errorMessage([]byte(tc.stderr), tc.code, "/r"); got != tc.want {
			t.Errorf("errorMessage(%q, %d) = %q, want %q", tc.stderr, tc.code, got, tc.want)
		}
	}
}

func TestErrorString(t *testing.T) {
	if got := (&Error{Op: "restore", Code: 1, Msg: "boom"}).Error(); got != "restic restore: boom (exit 1)" {
		t.Errorf("got %q", got)
	}
	if got := (&Error{Op: "snapshots", Code: -1, Msg: "boom"}).Error(); got != "restic snapshots: boom" {
		t.Errorf("got %q", got)
	}
}

func TestRedactRepo(t *testing.T) {
	cases := map[string]string{
		"rest:https://user:secret@host:8000/repo": "rest:https://user:xxxxx@host:8000/repo",
		"rest:https://user@host/repo":             "rest:https://user@host/repo",
		"sftp:user@host:/srv/repo":                "sftp:user@host:/srv/repo",
		"/srv/repo":                               "/srv/repo",
		`C:\backups\repo`:                         `C:\backups\repo`,
	}
	for in, want := range cases {
		if got := RedactRepo(in); got != want {
			t.Errorf("RedactRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMissingBinary(t *testing.T) {
	r := &Restic{Bin: filepath.Join(t.TempDir(), "no-such-restic"), Repo: "/r"}
	_, err := r.LatestSnapshot(context.Background())
	var re *Error
	if !errors.As(err, &re) || re.Code != -1 || !strings.Contains(re.Msg, "is restic installed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIntegrationLatestAndRestore(t *testing.T) {
	repo := testutil.NewRepo(t)
	src := t.TempDir()
	testutil.WriteFiles(t, src, map[string]string{"data/a.txt": "one", "b.txt": "two"})
	repo.Backup(t, src, "--tag", "app", "--time", "2026-01-01 10:00:00")
	testutil.WriteFiles(t, src, map[string]string{"data/a.txt": "one-v2"})
	repo.Backup(t, src, "--tag", "app", "--time", "2026-01-02 10:00:00")
	repo.Backup(t, src, "--tag", "other", "--time", "2026-01-03 10:00:00")

	r := &Restic{Repo: repo.Path, PasswordFile: repo.PasswordFile, Tag: "app"}
	ctx := context.Background()
	snap, err := r.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap.Time.Day() != 2 || len(snap.Tags) != 1 || snap.Tags[0] != "app" {
		t.Fatalf("expected the 2026-01-02 app snapshot, got %+v", snap)
	}
	if snap.ShortID == "" || len(snap.ID) < len(snap.ShortID) {
		t.Errorf("snapshot IDs missing: %+v", snap)
	}

	dest := t.TempDir()
	if err := r.Restore(ctx, snap, dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "data", "a.txt"))
	if err != nil || string(got) != "one-v2" {
		t.Fatalf("restored content = %q, %v", got, err)
	}

	locks, _ := os.ReadDir(filepath.Join(repo.Path, "locks"))
	if len(locks) != 0 {
		t.Errorf("drill must not leave lock files, found %d", len(locks))
	}
}

func TestIntegrationNoSnapshots(t *testing.T) {
	repo := testutil.NewRepo(t)
	r := &Restic{Repo: repo.Path, PasswordFile: repo.PasswordFile, Tag: "missing"}
	_, err := r.LatestSnapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), `no snapshots found`) || !strings.Contains(err.Error(), `"missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIntegrationWrongPassword(t *testing.T) {
	repo := testutil.NewRepo(t)
	bad := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(bad, []byte("nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Restic{Repo: repo.Path, PasswordFile: bad}
	_, err := r.LatestSnapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "nope") || strings.Contains(err.Error(), testutil.Password) {
		t.Fatalf("error leaks a password: %v", err)
	}
}

func TestIntegrationMissingRepo(t *testing.T) {
	testutil.RequireRestic(t)
	missing := filepath.Join(t.TempDir(), "nope")
	r := &Restic{Repo: missing, PasswordFile: os.DevNull}
	_, err := r.LatestSnapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "repository does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Error("drill must not create a missing repository")
	}
}
