package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func envWithPassword(k string) string {
	if k == "RESTIC_PASSWORD" {
		return "x"
	}
	return ""
}

func writePassFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "pass")
	if err := os.WriteFile(p, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseValid(t *testing.T) {
	dir := t.TempDir()
	writePassFile(t, dir)
	data := `
targets:
  - name: vw
    backend: restic
    repo: /srv/repo
    password_file: pass
    tag: vaultwarden
    checks:
      - max_age: 26h
      - exists: data/db.sqlite3
      - sqlite_integrity: ./data/db.sqlite3
      - min_files: 10
notify:
  webhook: https://ntfy.sh/topic
`
	cfg, err := Parse([]byte(data), filepath.Join(dir, "drill.yml"), noEnv)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("got %d targets", len(cfg.Targets))
	}
	tg := cfg.Targets[0]
	if tg.Name != "vw" || tg.Repo != "/srv/repo" || tg.Tag != "vaultwarden" || tg.Backend != "restic" {
		t.Errorf("unexpected target: %+v", tg)
	}
	if tg.PasswordFile != filepath.Join(dir, "pass") {
		t.Errorf("password file not resolved relative to config: %s", tg.PasswordFile)
	}
	want := []Check{
		{Type: CheckMaxAge, Duration: 26 * time.Hour},
		{Type: CheckExists, Path: "data/db.sqlite3"},
		{Type: CheckSQLiteIntegrity, Path: "data/db.sqlite3"},
		{Type: CheckMinFiles, Count: 10},
	}
	if len(tg.Checks) != len(want) {
		t.Fatalf("got %d checks", len(tg.Checks))
	}
	for i := range want {
		if tg.Checks[i] != want[i] {
			t.Errorf("check %d: got %+v, want %+v", i, tg.Checks[i], want[i])
		}
	}
	if cfg.Notify.Webhook != "https://ntfy.sh/topic" {
		t.Errorf("webhook: %q", cfg.Notify.Webhook)
	}
}

func TestParseEnvPassword(t *testing.T) {
	data := "targets:\n  - name: a\n    backend: restic\n    repo: /r\n"
	if _, err := Parse([]byte(data), "/x/drill.yml", envWithPassword); err != nil {
		t.Fatalf("RESTIC_PASSWORD should satisfy password requirement: %v", err)
	}
	if _, err := Parse([]byte(data), "/x/drill.yml", noEnv); err == nil {
		t.Fatal("expected error without any password source")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"no targets", "targets: []\n", "no targets defined"},
		{"unknown backend", "targets:\n  - name: a\n    backend: borg\n    repo: /r\n", `unknown backend "borg"`},
		{"missing backend", "targets:\n  - name: a\n    repo: /r\n", "missing `backend`"},
		{"missing repo", "targets:\n  - name: a\n    backend: restic\n", "missing `repo`"},
		{"missing name", "targets:\n  - backend: restic\n    repo: /r\n", "missing `name`"},
		{"bad name", "targets:\n  - name: a b\n    backend: restic\n    repo: /r\n", "must not contain spaces"},
		{"duplicate", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n  - name: a\n    backend: restic\n    repo: /r\n", "duplicate target name"},
		{"unknown check", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - md5: x\n", `unknown check "md5"`},
		{"bad duration", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - max_age: soon\n", `bad duration "soon"`},
		{"negative duration", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - max_age: -1h\n", "must be positive"},
		{"bad min_files", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - min_files: many\n", "positive integer"},
		{"two keys", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - exists: a\n        min_files: 2\n", "exactly one key"},
		{"abs path", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - exists: /etc/passwd\n", "must be relative"},
		{"escape path", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - exists: ../x\n", "inside the backed-up directory"},
		{"list value", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    checks:\n      - exists: [a, b]\n", "single non-empty value"},
		{"unknown field", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    passwd: x\n", "field passwd not found"},
		{"bad webhook", "targets:\n  - name: a\n    backend: restic\n    repo: /r\nnotify:\n  webhook: ntfy.sh/x\n", "not an http(s) URL"},
		{"missing password file", "targets:\n  - name: a\n    backend: restic\n    repo: /r\n    password_file: /does/not/exist\n", "cannot be read"},
		{"bad yaml", "targets: [\n", "invalid YAML"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml), "/x/drill.yml", envWithPassword)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestParseReportsAllProblems(t *testing.T) {
	data := "targets:\n  - name: a\n    backend: borg\n  - name: b\n    backend: restic\n    repo: /r\n    checks:\n      - nope: 1\n"
	_, err := Parse([]byte(data), "/x/drill.yml", envWithPassword)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	if len(ve.Problems) != 3 {
		t.Errorf("want 3 problems, got %d: %v", len(ve.Problems), ve.Problems)
	}
	if !strings.Contains(ve.Problems[0], "targets[0] (a)") {
		t.Errorf("problem should name the target: %q", ve.Problems[0])
	}
}

func TestParseDoesNotLeakPassword(t *testing.T) {
	dir := t.TempDir()
	pf := writePassFile(t, dir)
	data := "targets:\n  - name: a\n    backend: borg\n    repo: /r\n    password_file: " + pf + "\n"
	_, err := Parse([]byte(data), filepath.Join(dir, "drill.yml"), noEnv)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error must exist and not contain the password: %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "drill.yml")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "drill init") {
		t.Fatalf("missing file should suggest drill init, got %v", err)
	}
	t.Setenv("RESTIC_PASSWORD", "x")
	if err := os.WriteFile(p, []byte("targets:\n  - name: a\n    backend: restic\n    repo: /r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Path != p {
		t.Errorf("Path = %s, want %s", cfg.Path, p)
	}
}

func TestExampleIsValid(t *testing.T) {
	dir := t.TempDir()
	cfgText := strings.Replace(Example, "~/.restic-pass", writePassFile(t, dir), 1)
	if _, err := Parse([]byte(cfgText), filepath.Join(dir, "drill.yml"), noEnv); err != nil {
		t.Fatalf("example config should be valid once the password file exists: %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{"26h": 26 * time.Hour, "90m": 90 * time.Minute, "7d": 7 * 24 * time.Hour, "1.5d": 36 * time.Hour}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "d", "abc", "0h", "-2d"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should fail", bad)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{26 * time.Hour: "26h", 48 * time.Hour: "2d", 90 * time.Minute: "1h30m", 30 * time.Second: "30s", 10 * time.Minute: "10m"}
	for in, want := range cases {
		if got := FormatDuration(in); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckString(t *testing.T) {
	cases := map[Check]string{
		{Type: CheckMaxAge, Duration: 26 * time.Hour}:   "max_age 26h",
		{Type: CheckMinFiles, Count: 3}:                 "min_files 3",
		{Type: CheckExists, Path: "a/b"}:                "exists a/b",
		{Type: CheckSQLiteIntegrity, Path: "db.sqlite"}: "sqlite_integrity db.sqlite",
	}
	for c, want := range cases {
		if got := c.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", c, got, want)
		}
	}
}

func TestCleanRelPath(t *testing.T) {
	if got, err := cleanRelPath(`data\db.sqlite3`); err != nil || got != "data/db.sqlite3" {
		t.Errorf("backslashes should be normalised: %q %v", got, err)
	}
	for _, bad := range []string{"/abs", "..", "../x", "a/../../x", ".", `C:\x`} {
		if _, err := cleanRelPath(bad); err == nil {
			t.Errorf("cleanRelPath(%q) should fail", bad)
		}
	}
}

func TestResolvePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	base := t.TempDir()
	if got, _ := resolvePath("~/.pass", base); got != filepath.Join(home, ".pass") {
		t.Errorf("~ not expanded: %s", got)
	}
	if got, _ := resolvePath("p", base); got != filepath.Join(base, "p") {
		t.Errorf("relative not joined: %s", got)
	}
	abs := filepath.Join(base, "abs")
	if got, _ := resolvePath(abs, "/elsewhere"); got != abs {
		t.Errorf("absolute changed: %s", got)
	}
}
