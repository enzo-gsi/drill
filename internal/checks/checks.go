// Package checks verifies a restored snapshot.
package checks

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enzo-gsi/drill/internal/config"

	_ "modernc.org/sqlite" // pure Go SQLite driver, registered as "sqlite"
)

// Status is the outcome of a check.
type Status string

// Check outcomes.
const (
	Pass Status = "pass"
	Fail Status = "fail"
	Skip Status = "skip" // not run because an earlier step failed
)

// Result is the outcome of one check.
type Result struct {
	Name     string
	Status   Status
	Message  string
	Duration time.Duration
}

// Env is what checks run against.
type Env struct {
	Root         string    // directory the snapshot was restored into
	SnapshotTime time.Time // time of the restored snapshot
	Now          time.Time
}

// Run runs one configured check and times it.
func Run(ctx context.Context, c config.Check, env Env) Result {
	start := time.Now()
	var r Result
	switch c.Type {
	case config.CheckMaxAge:
		r = MaxAge(env.SnapshotTime, env.Now, c.Duration)
	case config.CheckExists:
		r = Exists(env.Root, c.Path)
	case config.CheckSQLiteIntegrity:
		r = SQLiteIntegrity(ctx, env.Root, c.Path)
	case config.CheckMinFiles:
		r = MinFiles(env.Root, c.Count)
	default:
		r = failf("unknown check type %q", c.Type)
	}
	r.Name = c.String()
	r.Duration = time.Since(start)
	return r
}

// MaxAge passes when the snapshot is younger than max.
func MaxAge(snapshot, now time.Time, max time.Duration) Result {
	age := now.Sub(snapshot)
	if age < 0 {
		age = 0
	}
	if age > max {
		return failf("latest snapshot is %s old, older than %s", FormatAge(age), config.FormatDuration(max))
	}
	return passf("latest snapshot is %s old (limit %s)", FormatAge(age), config.FormatDuration(max))
}

// Exists passes when rel exists inside root.
func Exists(root, rel string) Result {
	st, err := os.Stat(join(root, rel))
	if err != nil {
		return failf("%s is missing from the restored snapshot", rel)
	}
	if st.IsDir() {
		return passf("%s exists (directory)", rel)
	}
	return passf("%s exists (%s)", rel, FormatSize(st.Size()))
}

// SQLiteIntegrity opens rel read-only and runs PRAGMA integrity_check.
func SQLiteIntegrity(ctx context.Context, root, rel string) Result {
	p := join(root, rel)
	st, err := os.Stat(p)
	if err != nil {
		return failf("%s is missing from the restored snapshot", rel)
	}
	if !st.Mode().IsRegular() {
		return failf("%s is not a regular file", rel)
	}
	// SQLite treats empty or tiny files as empty databases, so check the header first.
	if !hasSQLiteHeader(p) {
		return failf("%s is not a SQLite database (missing \"SQLite format 3\" header)", rel)
	}
	db, err := sql.Open("sqlite", sqliteDSN(p))
	if err != nil {
		return failf("%s: cannot open: %v", rel, err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return failf("%s is corrupt: %v", rel, cleanSQLiteErr(err))
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return failf("%s is corrupt: %v", rel, cleanSQLiteErr(err))
		}
		// One row can hold several newline-separated problems.
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "***") {
				lines = append(lines, line)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return failf("%s is corrupt: %v", rel, cleanSQLiteErr(err))
	}
	if len(lines) == 1 && lines[0] == "ok" {
		return passf("%s integrity_check ok", rel)
	}
	if len(lines) > 3 {
		lines = append(lines[:3], fmt.Sprintf("and %d more problems", len(lines)-3))
	}
	return failf("%s failed integrity_check: %s", rel, strings.Join(lines, "; "))
}

// MinFiles passes when root holds at least n regular files.
func MinFiles(root string, n int) Result {
	count := 0
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			count++
		}
		return nil
	})
	if err != nil {
		return failf("cannot walk restored tree: %v", err)
	}
	if count < n {
		return failf("restored %d files, expected at least %d", count, n)
	}
	return passf("restored %d files (minimum %d)", count, n)
}

// FormatAge renders an age like "45s", "3h12m" or "4d2h".
func FormatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < 48*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if h == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days, h := int(d.Hours())/24, int(d.Hours())%24
		return fmt.Sprintf("%dd%dh", days, h)
	}
}

// FormatSize renders a byte count like "512 B" or "1.5 MB".
func FormatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// hasSQLiteHeader reports whether the file starts with the SQLite magic string.
func hasSQLiteHeader(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 16)
	_, err = io.ReadFull(f, buf)
	return err == nil && string(buf) == "SQLite format 3\x00"
}

// sqliteDSN builds a read-only SQLite URI for an absolute path.
func sqliteDSN(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows: C:/x -> /C:/x
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	return u.String()
}

// cleanSQLiteErr drops the driver's numeric suffix, e.g. "(26)".
func cleanSQLiteErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, " ("); i > 0 && strings.HasSuffix(s, ")") {
		s = s[:i]
	}
	return s
}

func join(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

func passf(format string, a ...any) Result {
	return Result{Status: Pass, Message: fmt.Sprintf(format, a...)}
}

func failf(format string, a ...any) Result {
	return Result{Status: Fail, Message: fmt.Sprintf(format, a...)}
}
