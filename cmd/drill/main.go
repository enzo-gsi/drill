// Command drill proves restic backups actually restore.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"

	"github.com/enzog/drill/internal/checks"
	"github.com/enzog/drill/internal/config"
	"github.com/enzog/drill/internal/report"
	"github.com/enzog/drill/internal/runner"
	"github.com/enzog/drill/internal/state"
)

// Exit codes.
const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

const usage = `drill: Your backups say 'success'. Prove it.

Usage:
  drill init                 write an example drill.yml
  drill run [target...]      restore the latest snapshot and run the checks
  drill list                 show targets and their last result

Flags:
  --config path   config file (default ./drill.yml)
  --json          machine-readable output

Exit codes: 0 all checks passed, 1 a check failed, 2 config or usage error.
`

type options struct {
	config string
	json   bool
	color  bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, useColor(os.Stdout))
	stop()
	os.Exit(code)
}

// run is main without the process globals, for tests.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, color bool) int {
	opts, pos, err := parseArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "drill: %v\n\n%s", err, usage)
		return exitUsage
	}
	opts.color = color && !opts.json
	if len(pos) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	cmd, rest := pos[0], pos[1:]
	switch cmd {
	case "init":
		return cmdInit(opts, rest, stdout, stderr)
	case "run":
		return cmdRun(ctx, opts, rest, stdout, stderr)
	case "list":
		return cmdList(opts, rest, stdout, stderr)
	case "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "drill: unknown command %q\n\n%s", cmd, usage)
		return exitUsage
	}
}

// parseArgs parses global flags, which may appear anywhere on the command line.
func parseArgs(args []string) (options, []string, error) {
	opts := options{}
	fs := flag.NewFlagSet("drill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.config, "config", "drill.yml", "config file")
	fs.BoolVar(&opts.json, "json", false, "machine-readable output")
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return opts, nil, err
		}
		if fs.NArg() == 0 {
			return opts, pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func cmdInit(opts options, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "drill init: unexpected argument %q\n", args[0])
		return exitUsage
	}
	f, err := os.OpenFile(opts.config, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		fmt.Fprintf(stderr, "drill init: %s already exists, refusing to overwrite it\n", opts.config)
		return exitUsage
	}
	if err != nil {
		fmt.Fprintf(stderr, "drill init: %v\n", err)
		return exitUsage
	}
	_, werr := f.WriteString(config.Example)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		fmt.Fprintf(stderr, "drill init: writing %s: %v\n", opts.config, werr)
		return exitUsage
	}
	fmt.Fprintf(stdout, "Wrote %s. Edit the repo, password and checks, then run `drill run`.\n", opts.config)
	return exitOK
}

func cmdRun(ctx context.Context, opts options, names []string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(opts.config)
	if err != nil {
		fmt.Fprintf(stderr, "drill: %v\n", err)
		return exitUsage
	}
	targets, err := selectTargets(cfg.Targets, names)
	if err != nil {
		fmt.Fprintf(stderr, "drill run: %v\n", err)
		return exitUsage
	}

	rep := runner.Runner{}.Run(ctx, targets)

	statePath := state.PathFor(cfg.Path)
	st, err := state.Load(statePath)
	if err != nil {
		fmt.Fprintf(stderr, "drill: warning: %v (starting a fresh state file)\n", err)
	}
	for _, t := range rep.Targets {
		st.Record(t)
	}
	if err := state.Save(statePath, st); err != nil {
		fmt.Fprintf(stderr, "drill: warning: %v\n", err)
	}

	if opts.json {
		err = report.WriteJSON(stdout, rep)
	} else {
		err = report.WriteText(stdout, rep, opts.color)
	}
	if err != nil {
		fmt.Fprintf(stderr, "drill: writing report: %v\n", err)
	}
	if rep.Status != checks.Pass {
		return exitFailed
	}
	return exitOK
}

// selectTargets returns the named targets, or all of them when names is empty.
func selectTargets(all []config.Target, names []string) ([]config.Target, error) {
	if len(names) == 0 {
		return all, nil
	}
	byName := map[string]config.Target{}
	var known []string
	for _, t := range all {
		byName[t.Name] = t
		known = append(known, t.Name)
	}
	var out []config.Target
	for _, n := range names {
		t, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("unknown target %q (configured: %s)", n, strings.Join(known, ", "))
		}
		out = append(out, t)
	}
	return out, nil
}

type listEntry struct {
	Name    string       `json:"name"`
	LastRun *state.Entry `json:"last_run"`
}

func cmdList(opts options, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "drill list: unexpected argument %q\n", args[0])
		return exitUsage
	}
	cfg, err := config.Load(opts.config)
	if err != nil {
		fmt.Fprintf(stderr, "drill: %v\n", err)
		return exitUsage
	}
	st, err := state.Load(state.PathFor(cfg.Path))
	if err != nil {
		fmt.Fprintf(stderr, "drill: warning: %v\n", err)
	}
	entries := make([]listEntry, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		le := listEntry{Name: t.Name}
		if e, ok := st.Targets[t.Name]; ok {
			le.LastRun = &e
		}
		entries = append(entries, le)
	}
	if opts.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"targets": entries}); err != nil {
			fmt.Fprintf(stderr, "drill: %v\n", err)
		}
		return exitOK
	}
	writeList(stdout, entries, opts.color)
	return exitOK
}

func writeList(w io.Writer, entries []listEntry, color bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tLAST RUN\tSTATUS\tSNAPSHOT\tFAILED CHECKS")
	for _, e := range entries {
		if e.LastRun == nil {
			fmt.Fprintf(tw, "%s\tnever\t-\t-\t-\n", e.Name)
			continue
		}
		status := strings.ToUpper(string(e.LastRun.Status))
		if color {
			code := "\x1b[32m"
			if e.LastRun.Status != checks.Pass {
				code = "\x1b[31m"
			}
			status = code + status + "\x1b[0m"
		}
		failed := "-"
		if len(e.LastRun.Failed) > 0 {
			failed = strings.Join(e.LastRun.Failed, ", ")
		}
		snap := e.LastRun.SnapshotID
		if snap == "" {
			snap = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.LastRun.RanAt.Local().Format("2006-01-02 15:04"), status, snap, failed)
	}
	tw.Flush()
}

// useColor reports whether f is a terminal and NO_COLOR is unset.
func useColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
