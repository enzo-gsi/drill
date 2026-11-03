// Package restic implements backend.Backend by shelling out to the restic CLI.
//
// Only read operations are used (`snapshots`, `restore`), always with
// --no-lock so drill never writes lock files into the repository.
package restic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/enzo-gsi/drill/internal/backend"
)

// Restic reads snapshots from one restic repository.
type Restic struct {
	Bin          string // restic executable, "restic" when empty
	Repo         string
	PasswordFile string // empty to use RESTIC_PASSWORD from the environment
	Tag          string // optional snapshot filter
}

var _ backend.Backend = (*Restic)(nil)

// Error is a failed restic invocation, with a message safe to show users.
type Error struct {
	Op   string // restic subcommand
	Code int    // restic exit code, -1 if it did not run
	Msg  string
}

func (e *Error) Error() string {
	if e.Code < 0 {
		return fmt.Sprintf("restic %s: %s", e.Op, e.Msg)
	}
	return fmt.Sprintf("restic %s: %s (exit %d)", e.Op, e.Msg, e.Code)
}

// LatestSnapshot returns the newest snapshot, filtered by Tag when set.
func (r *Restic) LatestSnapshot(ctx context.Context) (backend.Snapshot, error) {
	args := []string{"snapshots"}
	if r.Tag != "" {
		args = append(args, "--tag", r.Tag)
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return backend.Snapshot{}, err
	}
	snaps, err := parseSnapshots(out)
	if err != nil {
		return backend.Snapshot{}, &Error{Op: "snapshots", Code: 0, Msg: err.Error()}
	}
	latest, ok := newest(snaps)
	if !ok {
		msg := "no snapshots found in " + RedactRepo(r.Repo)
		if r.Tag != "" {
			msg += fmt.Sprintf(" with tag %q", r.Tag)
		}
		return backend.Snapshot{}, &Error{Op: "snapshots", Code: 0, Msg: msg}
	}
	return latest, nil
}

// Restore restores snap into dest. A snapshot with a single backup path is
// restored from that path, so its contents land directly under dest.
func (r *Restic) Restore(ctx context.Context, snap backend.Snapshot, dest string) error {
	_, err := r.run(ctx, "restore", restoreSpec(snap), "--target", dest)
	return err
}

func (r *Restic) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "restic"
	}
	full := []string{"--repo", r.Repo, "--no-lock", "--json"}
	if r.PasswordFile != "" {
		full = append(full, "--password-file", r.PasswordFile)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// cmd.Stdin stays nil so restic can never block on a password prompt.
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	op := args[0]
	if exitErr, ok := err.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		return nil, &Error{Op: op, Code: code, Msg: errorMessage(stderr.Bytes(), code, r.Repo)}
	}
	if ctx.Err() != nil {
		return nil, &Error{Op: op, Code: -1, Msg: "timed out or cancelled"}
	}
	return nil, &Error{Op: op, Code: -1, Msg: fmt.Sprintf("cannot run %s: %v (is restic installed and on PATH?)", bin, err)}
}

type resticSnapshot struct {
	ID       string   `json:"id"`
	ShortID  string   `json:"short_id"`
	Time     string   `json:"time"`
	Hostname string   `json:"hostname"`
	Paths    []string `json:"paths"`
	Tags     []string `json:"tags"`
}

func parseSnapshots(data []byte) ([]backend.Snapshot, error) {
	var raw []resticSnapshot
	if err := json.Unmarshal(bytes.TrimSpace(data), &raw); err != nil {
		return nil, fmt.Errorf("cannot parse snapshot list: %v", err)
	}
	snaps := make([]backend.Snapshot, 0, len(raw))
	for _, s := range raw {
		t, err := parseTime(s.Time)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s has invalid time %q", s.ShortID, s.Time)
		}
		snaps = append(snaps, backend.Snapshot{
			ID: s.ID, ShortID: s.ShortID, Time: t,
			Hostname: s.Hostname, Paths: s.Paths, Tags: s.Tags,
		})
	}
	return snaps, nil
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func newest(snaps []backend.Snapshot) (backend.Snapshot, bool) {
	if len(snaps) == 0 {
		return backend.Snapshot{}, false
	}
	best := snaps[0]
	for _, s := range snaps[1:] {
		if s.Time.After(best.Time) {
			best = s
		}
	}
	return best, true
}

// restoreSpec builds the `<id>[:<subfolder>]` argument for restic restore.
func restoreSpec(snap backend.Snapshot) string {
	if len(snap.Paths) != 1 {
		return snap.ID
	}
	return snap.ID + ":" + treePath(snap.Paths[0])
}

// treePath converts a snapshot path to restic's in-repository tree path.
// Windows paths like C:\data become /C/data.
func treePath(p string) string {
	if len(p) >= 2 && p[1] == ':' && isLetter(p[0]) {
		p = "/" + p[:1] + strings.ReplaceAll(p[2:], `\`, "/")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// errorMessage extracts a short, user-facing message from restic's stderr.
func errorMessage(stderr []byte, code int, repo string) string {
	switch code {
	case 10:
		return "repository does not exist at " + RedactRepo(repo)
	case 12:
		return "wrong password or no key found"
	}
	msg := ""
	for _, line := range strings.Split(string(stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var je struct {
			MessageType string `json:"message_type"`
			Message     string `json:"message"`
		}
		if json.Unmarshal([]byte(line), &je) == nil && je.Message != "" {
			if je.MessageType == "exit_error" || msg == "" {
				msg = je.Message
			}
			continue
		}
		msg = line
	}
	msg = strings.TrimPrefix(msg, "Fatal: ")
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	if msg == "" {
		msg = "failed without an error message"
	}
	return msg
}

// RedactRepo hides any password embedded in a repository URL, such as
// rest:https://user:secret@host/repo.
func RedactRepo(repo string) string {
	prefix, rest := "", repo
	for _, p := range []string{"rest:", "s3:", "swift:", "azure:", "gs:", "rclone:"} {
		if strings.HasPrefix(repo, p) {
			prefix, rest = p, repo[len(p):]
			break
		}
	}
	u, err := url.Parse(rest)
	if err != nil || u.User == nil {
		return repo
	}
	if _, hasPass := u.User.Password(); !hasPass {
		return repo
	}
	u.User = url.UserPassword(u.User.Username(), "xxxxx")
	return prefix + u.String()
}
