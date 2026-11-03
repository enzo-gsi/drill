// Package config loads and validates drill.yml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Check types supported by drill.
const (
	CheckMaxAge          = "max_age"
	CheckExists          = "exists"
	CheckSQLiteIntegrity = "sqlite_integrity"
	CheckMinFiles        = "min_files"
)

// Backends supported by drill.
const BackendRestic = "restic"

var validChecks = []string{CheckMaxAge, CheckExists, CheckSQLiteIntegrity, CheckMinFiles}

// Config is a parsed and validated drill.yml.
type Config struct {
	Targets []Target
	Notify  Notify
	// Path is the absolute path of the file the config was loaded from.
	Path string
}

// Target is one backup repository to drill.
type Target struct {
	Name         string
	Backend      string
	Repo         string
	PasswordFile string // absolute path, empty when RESTIC_PASSWORD is used
	Tag          string
	Checks       []Check
}

// Check is one validated check of a target.
type Check struct {
	Type     string
	Path     string        // exists, sqlite_integrity: slash-separated, relative
	Duration time.Duration // max_age
	Count    int           // min_files
}

// String renders the check the way it is written in the config.
func (c Check) String() string {
	switch c.Type {
	case CheckMaxAge:
		return c.Type + " " + FormatDuration(c.Duration)
	case CheckMinFiles:
		return c.Type + " " + strconv.Itoa(c.Count)
	default:
		return c.Type + " " + c.Path
	}
}

// Notify holds notification settings.
type Notify struct {
	Webhook string
}

type rawConfig struct {
	Targets []rawTarget `yaml:"targets"`
	Notify  struct {
		Webhook string `yaml:"webhook"`
	} `yaml:"notify"`
}

type rawTarget struct {
	Name         string                 `yaml:"name"`
	Backend      string                 `yaml:"backend"`
	Repo         string                 `yaml:"repo"`
	PasswordFile string                 `yaml:"password_file"`
	Tag          string                 `yaml:"tag"`
	Checks       []map[string]yaml.Node `yaml:"checks"`
}

// Env abstracts environment lookups so validation can be tested.
type Env func(key string) string

// Load reads and validates the config file at p.
func Load(p string) (*Config, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file %s not found (create one with `drill init`)", p)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}
	return Parse(data, abs, os.Getenv)
}

// Parse validates config data. configPath is used to resolve relative
// password files and is stored in Config.Path.
func Parse(data []byte, configPath string, getenv Env) (*Config, error) {
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", configPath, err)
	}

	cfg := &Config{Path: configPath, Notify: Notify{Webhook: raw.Notify.Webhook}}
	var errs []string
	if len(raw.Targets) == 0 {
		errs = append(errs, "no targets defined: add at least one entry under `targets:`")
	}
	seen := map[string]bool{}
	for i, rt := range raw.Targets {
		t, terrs := parseTarget(rt, filepath.Dir(configPath), getenv)
		label := fmt.Sprintf("targets[%d]", i)
		if rt.Name != "" {
			label += fmt.Sprintf(" (%s)", rt.Name)
			if seen[rt.Name] {
				terrs = append(terrs, fmt.Sprintf("duplicate target name %q", rt.Name))
			}
			seen[rt.Name] = true
		}
		for _, e := range terrs {
			errs = append(errs, label+": "+e)
		}
		cfg.Targets = append(cfg.Targets, t)
	}
	if w := cfg.Notify.Webhook; w != "" {
		u, err := url.Parse(w)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Sprintf("notify.webhook: %q is not an http(s) URL", w))
		}
	}
	if len(errs) > 0 {
		return nil, &ValidationError{Path: configPath, Problems: errs}
	}
	return cfg, nil
}

// ValidationError lists every problem found in a config file.
type ValidationError struct {
	Path     string
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: invalid config:\n  - %s", e.Path, strings.Join(e.Problems, "\n  - "))
}

func parseTarget(rt rawTarget, baseDir string, getenv Env) (Target, []string) {
	var errs []string
	t := Target{Name: rt.Name, Backend: rt.Backend, Repo: rt.Repo, Tag: rt.Tag}
	if t.Name == "" {
		errs = append(errs, "missing `name`")
	} else if strings.ContainsAny(t.Name, " \t/\\") {
		errs = append(errs, fmt.Sprintf("name %q must not contain spaces or slashes", t.Name))
	}
	switch t.Backend {
	case "":
		errs = append(errs, "missing `backend` (supported: restic)")
	case BackendRestic:
	default:
		errs = append(errs, fmt.Sprintf("unknown backend %q (supported: restic)", t.Backend))
	}
	if t.Repo == "" {
		errs = append(errs, "missing `repo` (path or URL of the restic repository)")
	}
	if rt.PasswordFile != "" {
		pf, err := resolvePath(rt.PasswordFile, baseDir)
		if err != nil {
			errs = append(errs, "password_file: "+err.Error())
		} else if st, err := os.Stat(pf); err != nil {
			errs = append(errs, fmt.Sprintf("password_file %s cannot be read: %v", pf, errors.Unwrap(err)))
		} else if st.IsDir() {
			errs = append(errs, fmt.Sprintf("password_file %s is a directory", pf))
		}
		t.PasswordFile = pf
	} else if getenv("RESTIC_PASSWORD") == "" && getenv("RESTIC_PASSWORD_FILE") == "" {
		errs = append(errs, "missing `password_file` (or set RESTIC_PASSWORD in the environment)")
	}
	for j, node := range rt.Checks {
		c, err := parseCheck(node)
		if err != nil {
			errs = append(errs, fmt.Sprintf("checks[%d]: %v", j, err))
			continue
		}
		t.Checks = append(t.Checks, c)
	}
	return t, errs
}

func parseCheck(m map[string]yaml.Node) (Check, error) {
	if len(m) != 1 {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return Check{}, fmt.Errorf("each check must have exactly one key, got %v (write one `- check: value` per line)", keys)
	}
	var typ string
	var node yaml.Node
	for k, v := range m {
		typ, node = k, v
	}
	if node.Kind != yaml.ScalarNode || node.Value == "" {
		return Check{}, fmt.Errorf("%s: value must be a single non-empty value", typ)
	}
	val := node.Value
	c := Check{Type: typ}
	switch typ {
	case CheckMaxAge:
		d, err := ParseDuration(val)
		if err != nil {
			return Check{}, fmt.Errorf("max_age: %v", err)
		}
		c.Duration = d
	case CheckExists, CheckSQLiteIntegrity:
		p, err := cleanRelPath(val)
		if err != nil {
			return Check{}, fmt.Errorf("%s: %v", typ, err)
		}
		c.Path = p
	case CheckMinFiles:
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			return Check{}, fmt.Errorf("min_files: %q must be a positive integer", val)
		}
		c.Count = n
	default:
		return Check{}, fmt.Errorf("unknown check %q (valid: %s)", typ, strings.Join(validChecks, ", "))
	}
	return c, nil
}

// ParseDuration parses Go durations ("26h", "90m") plus a day suffix ("7d").
func ParseDuration(s string) (time.Duration, error) {
	var d time.Duration
	var err error
	if n, ok := strings.CutSuffix(s, "d"); ok {
		var days float64
		days, err = strconv.ParseFloat(n, 64)
		d = time.Duration(days * float64(24*time.Hour))
	} else {
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, fmt.Errorf("bad duration %q (examples: 26h, 90m, 7d)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
}

// FormatDuration renders a duration compactly, using days when exact.
func FormatDuration(d time.Duration) string {
	day := 24 * time.Hour
	if d >= day && d%day == 0 {
		return strconv.FormatInt(int64(d/day), 10) + "d"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// cleanRelPath ensures p is relative and cannot escape the restore root.
func cleanRelPath(p string) (string, error) {
	p = filepath.ToSlash(p)
	if path.IsAbs(p) || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("path %q must be relative to the backed-up directory", p)
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("path %q must point inside the backed-up directory", p)
	}
	return c, nil
}

// resolvePath expands a leading ~ and makes p absolute relative to baseDir.
func resolvePath(p, baseDir string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~: %v", err)
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	return filepath.Clean(p), nil
}

// Example is the config written by `drill init`.
const Example = `# drill configuration. Docs: https://github.com/enzog/drill
targets:
  - name: vaultwarden
    backend: restic
    repo: /path/or/sftp:user@host:/repo
    password_file: ~/.restic-pass     # or env RESTIC_PASSWORD
    tag: vaultwarden                  # optional snapshot filter
    checks:
      - max_age: 26h
      - exists: data/db.sqlite3
      - sqlite_integrity: data/db.sqlite3
      - min_files: 10
notify:
  webhook: https://ntfy.sh/my-topic   # optional, POSTed on failure
`
