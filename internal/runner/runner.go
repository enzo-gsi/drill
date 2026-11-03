// Package runner drills targets: find the latest snapshot, restore it into a
// temp dir, run the checks, and always clean up.
package runner

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/enzo-gsi/drill/internal/backend"
	"github.com/enzo-gsi/drill/internal/backend/restic"
	"github.com/enzo-gsi/drill/internal/checks"
	"github.com/enzo-gsi/drill/internal/config"
	"github.com/enzo-gsi/drill/internal/report"
)

// Implicit check names, reported before the configured checks.
const (
	CheckSnapshot = "snapshot"
	CheckRestore  = "restore"
)

// Runner drills targets.
type Runner struct {
	// NewBackend builds the backend for a target; defaults to NewBackend.
	NewBackend func(config.Target) backend.Backend
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
	// TempDir is where restores happen; defaults to os.TempDir().
	TempDir string
}

// NewBackend returns the backend for a validated target.
func NewBackend(t config.Target) backend.Backend {
	return &restic.Restic{Repo: t.Repo, PasswordFile: t.PasswordFile, Tag: t.Tag}
}

// Run drills every target in order.
func (r Runner) Run(ctx context.Context, targets []config.Target) report.Report {
	rep := report.Report{StartedAt: r.now()}
	for _, t := range targets {
		rep.Targets = append(rep.Targets, r.RunTarget(ctx, t))
	}
	rep.Finish(r.now())
	return rep
}

// RunTarget drills one target.
func (r Runner) RunTarget(ctx context.Context, t config.Target) (out report.Target) {
	start := r.now()
	out = report.Target{Name: t.Name, StartedAt: start}
	defer func() {
		out.DurationMS = r.now().Sub(start).Milliseconds()
		out.Status = checks.Pass
		for _, c := range out.Checks {
			if c.Status != checks.Pass {
				out.Status = checks.Fail
			}
		}
	}()

	newBackend := r.NewBackend
	if newBackend == nil {
		newBackend = NewBackend
	}
	b := newBackend(t)

	step := time.Now()
	snap, err := b.LatestSnapshot(ctx)
	if err != nil {
		out.Checks = append(out.Checks, implicit(CheckSnapshot, checks.Fail, err.Error(), step))
		out.Checks = append(out.Checks, report.Check{Name: CheckRestore, Status: checks.Skip, Message: "not run: no snapshot to restore"})
		out.Checks = append(out.Checks, skipped(t.Checks, "no snapshot to check")...)
		return out
	}
	out.Snapshot = &report.Snapshot{ID: snap.ID, ShortID: snap.ShortID, Time: snap.Time}
	out.Checks = append(out.Checks, implicit(CheckSnapshot, checks.Pass,
		fmt.Sprintf("latest snapshot %s from %s", snap.ShortID, snap.Time.Local().Format("2006-01-02 15:04")), step))

	dir, err := os.MkdirTemp(r.TempDir, "drill-"+t.Name+"-")
	if err != nil {
		out.Checks = append(out.Checks, implicit(CheckRestore, checks.Fail, "cannot create temp dir: "+err.Error(), time.Now()))
		out.Checks = append(out.Checks, skipped(t.Checks, "restore failed")...)
		return out
	}
	defer func() {
		if err := removeAll(dir); err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("could not delete temp dir %s: %v", dir, err))
		}
	}()

	step = time.Now()
	env := checks.Env{Root: dir, SnapshotTime: snap.Time, Now: r.now()}
	if err := b.Restore(ctx, snap, dir); err != nil {
		out.Checks = append(out.Checks, implicit(CheckRestore, checks.Fail, err.Error(), step))
		for _, c := range t.Checks {
			if c.Type == config.CheckMaxAge {
				// max_age needs only the snapshot time, so it still runs.
				out.Checks = append(out.Checks, report.FromResult(checks.Run(ctx, c, env)))
			} else {
				out.Checks = append(out.Checks, skipped([]config.Check{c}, "restore failed")...)
			}
		}
		return out
	}
	out.Checks = append(out.Checks, implicit(CheckRestore, checks.Pass,
		fmt.Sprintf("restored into a temp dir in %s", time.Since(step).Round(10*time.Millisecond)), step))

	for _, c := range t.Checks {
		out.Checks = append(out.Checks, report.FromResult(checks.Run(ctx, c, env)))
	}
	return out
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func implicit(name string, status checks.Status, msg string, start time.Time) report.Check {
	return report.Check{Name: name, Status: status, Message: msg, DurationMS: time.Since(start).Milliseconds()}
}

// skipped marks configured checks as not run.
func skipped(cs []config.Check, reason string) []report.Check {
	out := make([]report.Check, 0, len(cs))
	for _, c := range cs {
		out = append(out, report.Check{Name: c.String(), Status: checks.Skip, Message: "not run: " + reason})
	}
	return out
}

// removeAll deletes dir, making read-only restored files writable if needed.
func removeAll(dir string) error {
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}
