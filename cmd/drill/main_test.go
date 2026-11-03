package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzog/drill/internal/config"
	"github.com/enzog/drill/internal/state"
	"github.com/enzog/drill/internal/testutil"
)

type result struct {
	code           int
	stdout, stderr string
}

func drill(t *testing.T, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, &out, &errOut, false)
	return result{code, out.String(), errOut.String()}
}

// fixture is a throwaway repo with one good backup and a config pointing at it.
type fixture struct {
	repo   testutil.Repo
	src    string
	config string
}

func newFixture(t *testing.T, checks string) fixture {
	t.Helper()
	repo := testutil.NewRepo(t)
	src := t.TempDir()
	testutil.WriteFiles(t, src, map[string]string{"data/notes.txt": "hello", "data/b.txt": "b", "c.txt": "c"})
	testutil.CreateSQLite(t, filepath.Join(src, "data", "db.sqlite3"))
	f := fixture{repo: repo, src: src, config: filepath.Join(t.TempDir(), "drill.yml")}
	f.writeConfig(t, repo.PasswordFile, checks)
	return f
}

func (f fixture) writeConfig(t *testing.T, passwordFile, checks string) {
	t.Helper()
	cfg := fmt.Sprintf("targets:\n  - name: app\n    backend: restic\n    repo: %q\n    password_file: %q\n    tag: app\n    checks:\n%s",
		f.repo.Path, passwordFile, checks)
	if err := os.WriteFile(f.config, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

const allChecks = `      - max_age: 26h
      - exists: data/notes.txt
      - sqlite_integrity: data/db.sqlite3
      - min_files: 4
`

func TestUsage(t *testing.T) {
	if r := drill(t); r.code != exitUsage || !strings.Contains(r.stderr, "Usage:") {
		t.Errorf("no args: %+v", r)
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		if r := drill(t, args...); r.code != exitOK || !strings.Contains(r.stdout, "drill run") {
			t.Errorf("%v: %+v", args, r)
		}
	}
	if r := drill(t, "frobnicate"); r.code != exitUsage || !strings.Contains(r.stderr, `unknown command "frobnicate"`) {
		t.Errorf("unknown command: %+v", r)
	}
	if r := drill(t, "run", "--nope"); r.code != exitUsage || !strings.Contains(r.stderr, "flag provided but not defined") {
		t.Errorf("bad flag: %+v", r)
	}
}

func TestParseArgs(t *testing.T) {
	opts, pos, err := parseArgs([]string{"run", "--json", "app", "--config", "x.yml", "other"})
	if err != nil || !opts.json || opts.config != "x.yml" || strings.Join(pos, ",") != "run,app,other" {
		t.Errorf("got %+v %v %v", opts, pos, err)
	}
	opts, _, _ = parseArgs(nil)
	if opts.config != "drill.yml" || opts.json {
		t.Errorf("defaults: %+v", opts)
	}
}

func TestInit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "drill.yml")
	r := drill(t, "init", "--config", p)
	if r.code != exitOK || !strings.Contains(r.stdout, "Wrote") {
		t.Fatalf("init: %+v", r)
	}
	data, _ := os.ReadFile(p)
	if string(data) != config.Example {
		t.Error("init did not write the example config")
	}
	os.WriteFile(p, []byte("mine"), 0o644)
	r = drill(t, "init", "--config", p)
	if r.code != exitUsage || !strings.Contains(r.stderr, "refusing to overwrite") {
		t.Errorf("second init: %+v", r)
	}
	if data, _ := os.ReadFile(p); string(data) != "mine" {
		t.Error("init overwrote an existing file")
	}
	if r := drill(t, "init", "extra"); r.code != exitUsage {
		t.Errorf("init with argument: %+v", r)
	}
	if r := drill(t, "init", "--config", filepath.Join(t.TempDir(), "no", "dir.yml")); r.code != exitUsage {
		t.Errorf("init into missing dir: %+v", r)
	}
}

func TestRunPass(t *testing.T) {
	f := newFixture(t, allChecks)
	f.repo.Backup(t, f.src, "--tag", "app")
	r := drill(t, "run", "--config", f.config)
	if r.code != exitOK {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{"PASS  snapshot", "PASS  restore", "PASS  max_age 26h", "PASS  exists data/notes.txt", "PASS  sqlite_integrity data/db.sqlite3", "PASS  min_files 4", "app: PASS, 6 checks passed"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q in:\n%s", want, r.stdout)
		}
	}
	st, err := state.Load(state.PathFor(f.config))
	if err != nil || st.Targets["app"].Status != "pass" {
		t.Errorf("state not recorded: %+v %v", st, err)
	}
}

func TestRunJSON(t *testing.T) {
	f := newFixture(t, allChecks)
	f.repo.Backup(t, f.src, "--tag", "app")
	r := drill(t, "--json", "run", "app", "--config", f.config)
	if r.code != exitOK {
		t.Fatalf("exit %d: %s %s", r.code, r.stdout, r.stderr)
	}
	var got struct {
		SchemaVersion int    `json:"schema_version"`
		Status        string `json:"status"`
		Targets       []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			Snapshot struct {
				ID      string    `json:"id"`
				ShortID string    `json:"short_id"`
				Time    time.Time `json:"time"`
			} `json:"snapshot"`
			Checks []struct {
				Name, Status, Message string
				DurationMS            *int64 `json:"duration_ms"`
			} `json:"checks"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, r.stdout)
	}
	if got.SchemaVersion != 1 || got.Status != "pass" || len(got.Targets) != 1 {
		t.Fatalf("got %+v", got)
	}
	tg := got.Targets[0]
	if tg.Snapshot.ID == "" || tg.Snapshot.ShortID == "" || tg.Snapshot.Time.IsZero() || len(tg.Checks) != 6 || tg.Checks[0].DurationMS == nil {
		t.Errorf("target: %+v", tg)
	}
	if strings.Contains(r.stdout, testutil.Password) {
		t.Error("JSON output leaks the password")
	}
}

func TestRunUnknownTarget(t *testing.T) {
	f := newFixture(t, allChecks)
	r := drill(t, "run", "nope", "--config", f.config)
	if r.code != exitUsage || !strings.Contains(r.stderr, `unknown target "nope" (configured: app)`) {
		t.Errorf("got %+v", r)
	}
}

func TestRunMissingConfig(t *testing.T) {
	r := drill(t, "run", "--config", filepath.Join(t.TempDir(), "drill.yml"))
	if r.code != exitUsage || !strings.Contains(r.stderr, "drill init") {
		t.Errorf("got %+v", r)
	}
}

func TestList(t *testing.T) {
	f := newFixture(t, allChecks)
	r := drill(t, "list", "--config", f.config)
	if r.code != exitOK || !strings.Contains(r.stdout, "app     never") {
		t.Fatalf("before run: %+v", r)
	}
	f.repo.Backup(t, f.src, "--tag", "app")
	drill(t, "run", "--config", f.config)
	r = drill(t, "list", "--config", f.config)
	if r.code != exitOK || !strings.Contains(r.stdout, "PASS") {
		t.Fatalf("after run: %+v", r)
	}
	r = drill(t, "list", "--json", "--config", f.config)
	var got struct {
		Targets []struct {
			Name    string       `json:"name"`
			LastRun *state.Entry `json:"last_run"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || len(got.Targets) != 1 || got.Targets[0].LastRun == nil || got.Targets[0].LastRun.SnapshotID == "" {
		t.Errorf("list --json: %v %s", err, r.stdout)
	}
	if r := drill(t, "list", "extra", "--config", f.config); r.code != exitUsage {
		t.Errorf("list with argument: %+v", r)
	}
	if r := drill(t, "list", "--config", filepath.Join(t.TempDir(), "x.yml")); r.code != exitUsage {
		t.Errorf("list without config: %+v", r)
	}
}

func TestSelectTargets(t *testing.T) {
	all := []config.Target{{Name: "a"}, {Name: "b"}}
	if got, _ := selectTargets(all, nil); len(got) != 2 {
		t.Errorf("all: %v", got)
	}
	if got, _ := selectTargets(all, []string{"b"}); len(got) != 1 || got[0].Name != "b" {
		t.Errorf("b: %v", got)
	}
	if _, err := selectTargets(all, []string{"c"}); err == nil || !strings.Contains(err.Error(), "configured: a, b") {
		t.Errorf("c: %v", err)
	}
}

func TestWriteList(t *testing.T) {
	var buf bytes.Buffer
	ran := time.Date(2026, 1, 2, 3, 4, 0, 0, time.Local)
	writeList(&buf, []listEntry{
		{Name: "a"},
		{Name: "b", LastRun: &state.Entry{Status: "fail", RanAt: ran, Failed: []string{"restore", "exists x"}}},
	}, true)
	out := buf.String()
	for _, want := range []string{"TARGET", "never", "2026-01-02 03:04", "\x1b[31mFAIL\x1b[0m", "restore, exists x"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestUseColor(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if useColor(f) {
		t.Error("a regular file is not a terminal")
	}
	t.Setenv("NO_COLOR", "1")
	if useColor(os.Stdout) {
		t.Error("NO_COLOR must disable colour")
	}
}
