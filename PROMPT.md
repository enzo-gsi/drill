Build "drill" v0.1, a Go CLI that verifies restic backups by restoring the latest snapshot into a temp dir and running checks on it. Read CLAUDE.md first and follow its rules strictly.

## Commands
- `drill init`            write an example drill.yml in the current directory (refuse to overwrite)
- `drill run [target]`    run the drill for one target, or all targets if none given
- `drill list`            list targets and the result of their last run (store last results in a small JSON state file next to the config)
Global flags: `--config path` (default ./drill.yml), `--json` (machine-readable output).

## Config (drill.yml)
```yaml
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
  webhook: https://ntfy.sh/my-topic   # optional
```
Validate the config with clear, actionable error messages (unknown backend, unknown check, missing fields, bad duration).

## Checks
- `max_age`: latest matching snapshot must be younger than the duration.
- `exists`: path must exist inside the restored tree.
- `sqlite_integrity`: open the restored file read-only and run `PRAGMA integrity_check`; pass only if the result is "ok".
- `min_files`: restored tree contains at least N regular files.
The restore itself succeeding is an implicit check for every target.

## Output and exit codes
- Human output: one line per check with PASS/FAIL, then a summary per target. Use colour only if stdout is a TTY.
- `--json`: stable schema (targets, checks, durations, snapshot id and time, overall status).
- Exit codes: 0 all passed, 1 at least one check failed, 2 config or usage error.
- On failure, if notify.webhook is set, POST a short JSON payload (target, failed checks, snapshot id). Never include secrets.

## Architecture
cmd/drill, internal/config, internal/backend/restic, internal/checks, internal/report, internal/notify, internal/state.
Define a small Backend interface (LatestSnapshot, Restore) so Borg can be added later, but implement only restic.

## Milestones (commit after each, run gofmt + go vet + go test first)
1. Project skeleton, go.mod, config parsing and validation, with tests.
2. restic backend: latest snapshot (JSON) and restore to a temp dir. Integration tests build a throwaway local restic repo in t.TempDir() using RESTIC_PASSWORD; skip the test if restic is not installed.
3. Checks: max_age, exists, sqlite_integrity, min_files, each with tests.
4. Runner plus terminal and JSON reports, exit codes, state file, `list`.
5. Webhook notification (test it against httptest.Server).
6. Failure-case tests that prove drill catches real problems: a corrupted SQLite file, a missing file, an old snapshot, a wrong password, a missing repo. Each must produce exit code 1 (or 2 for config errors) and a clear message.
7. README.md (English, tagline first, one-line install, quickstart, config reference, safety guarantees), MIT LICENSE, a GitHub Actions workflow running gofmt check, go vet and go test, and `scripts/demo.sh`: create a temp restic repo, back up sample data containing a SQLite DB, run drill (passes), corrupt the DB in the repo's source data, back up again, run drill (fails in red). This script is for recording a demo GIF.

## Working rules
- Work autonomously through all milestones in order. If a choice is ambiguous, pick the simplest reasonable option, note it under "Decisions" in BUILD_LOG.md, and continue. Stop and ask only if something would require a destructive action, network access beyond `go mod download`, or leaving this directory.
- Do not add features outside the scope above.
- Keep BUILD_LOG.md updated: milestones done, decisions, known limitations, and a final "what to do next" list.

## Definition of done
`gofmt -l .` prints nothing, `go vet ./...` and `go test ./...` pass, `scripts/demo.sh` runs end to end, and BUILD_LOG.md summarises the work. Finish with a short report of what was built and what is still missing.
