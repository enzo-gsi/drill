// Package report renders drill results for humans and machines.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/enzog/drill/internal/checks"
)

// SchemaVersion is bumped on any breaking change to the JSON output.
const SchemaVersion = 1

// Report is the result of one `drill run`.
type Report struct {
	SchemaVersion int           `json:"schema_version"`
	Status        checks.Status `json:"status"`
	StartedAt     time.Time     `json:"started_at"`
	DurationMS    int64         `json:"duration_ms"`
	Targets       []Target      `json:"targets"`
}

// Target is the result for one target.
type Target struct {
	Name       string        `json:"name"`
	Status     checks.Status `json:"status"`
	StartedAt  time.Time     `json:"started_at"`
	DurationMS int64         `json:"duration_ms"`
	Snapshot   *Snapshot     `json:"snapshot"`
	Checks     []Check       `json:"checks"`
	Warnings   []string      `json:"warnings,omitempty"`
}

// Snapshot identifies the snapshot that was drilled.
type Snapshot struct {
	ID      string    `json:"id"`
	ShortID string    `json:"short_id"`
	Time    time.Time `json:"time"`
}

// Check is the result of one check.
type Check struct {
	Name       string        `json:"name"`
	Status     checks.Status `json:"status"`
	Message    string        `json:"message"`
	DurationMS int64         `json:"duration_ms"`
}

// FromResult converts a check result for reporting.
func FromResult(r checks.Result) Check {
	return Check{Name: r.Name, Status: r.Status, Message: r.Message, DurationMS: r.Duration.Milliseconds()}
}

// Failed returns the checks that did not pass, skipped ones excluded.
func (t Target) Failed() []Check {
	var out []Check
	for _, c := range t.Checks {
		if c.Status == checks.Fail {
			out = append(out, c)
		}
	}
	return out
}

// Finish sets the overall status from the targets.
func (r *Report) Finish(end time.Time) {
	r.SchemaVersion = SchemaVersion
	r.DurationMS = end.Sub(r.StartedAt).Milliseconds()
	r.Status = checks.Pass
	for _, t := range r.Targets {
		if t.Status != checks.Pass {
			r.Status = checks.Fail
		}
	}
}

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r Report) error {
	if r.Targets == nil {
		r.Targets = []Target{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

const (
	green = "\x1b[32m"
	red   = "\x1b[31m"
	gray  = "\x1b[90m"
	bold  = "\x1b[1m"
	reset = "\x1b[0m"
)

// WriteText writes a human-readable report, coloured when color is true.
func WriteText(w io.Writer, r Report, color bool) error {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + reset
	}
	var b strings.Builder
	passed, failed := 0, 0
	for i, t := range r.Targets {
		if i > 0 {
			b.WriteString("\n")
		}
		header := paint(bold, t.Name)
		if t.Snapshot != nil {
			header += fmt.Sprintf("  snapshot %s from %s", t.Snapshot.ShortID, t.Snapshot.Time.Local().Format("2006-01-02 15:04"))
		}
		b.WriteString(header + "\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, c := range t.Checks {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", statusLabel(c.Status, paint), c.Name, c.Message)
		}
		tw.Flush()
		for _, warn := range t.Warnings {
			b.WriteString("  " + paint(gray, "warning: "+warn) + "\n")
		}
		dur := time.Duration(t.DurationMS) * time.Millisecond
		if t.Status == checks.Pass {
			passed++
			fmt.Fprintf(&b, "%s: %s, %d checks passed in %s\n", t.Name, paint(green, "PASS"), len(t.Checks), dur.Round(100*time.Millisecond))
		} else {
			failed++
			fmt.Fprintf(&b, "%s: %s, %d of %d checks failed in %s\n", t.Name, paint(red, "FAIL"), len(t.Failed()), len(t.Checks), dur.Round(100*time.Millisecond))
		}
	}
	if len(r.Targets) > 1 {
		summary := fmt.Sprintf("\n%d targets: %d passed, %d failed\n", len(r.Targets), passed, failed)
		if failed > 0 {
			summary = paint(red, summary)
		}
		b.WriteString(summary)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func statusLabel(s checks.Status, paint func(string, string) string) string {
	switch s {
	case checks.Pass:
		return paint(green, "PASS")
	case checks.Fail:
		return paint(red, "FAIL")
	default:
		return paint(gray, "SKIP")
	}
}
