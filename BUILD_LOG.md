# drill build log

## Milestones
- [x] 1. Project skeleton, go.mod, config parsing and validation, with tests.
- [x] 2. restic backend: latest snapshot and restore to a temp dir, with integration tests on throwaway repos.
- [x] 3. Checks: max_age, exists, sqlite_integrity, min_files, each with tests.
- [x] 4. Runner, terminal and JSON reports, exit codes, state file, `drill list`.

## Decisions
- Module path is `github.com/enzog/drill` (placeholder until the real GitHub repo exists).
- YAML parsing uses `gopkg.in/yaml.v3` (the standard library has no YAML parser). Unknown fields are rejected so typos surface as errors.
- `max_age` accepts Go durations (`26h`, `90m`) plus a `d` suffix for days (`7d`).
- A relative `password_file` is resolved against the config file's directory; `~` is expanded.
- A target needs `password_file`, or `RESTIC_PASSWORD` / `RESTIC_PASSWORD_FILE` in the environment.
- Check paths must be relative and must not escape the restored tree (`..` and absolute paths are rejected).
- Config validation reports every problem at once, each prefixed with the target index and name.
- restic is always called with `--no-lock`, so drill never writes lock files into the repository (verified in tests).
- The latest snapshot is picked by time from `restic snapshots --json` (filtered by `--tag`), not with `--latest`, which groups by host and path.
- A snapshot with a single backup path is restored with restic's `<id>:<path>` subfolder syntax, so its contents land directly in the temp dir and check paths are relative to the backed-up directory. Snapshots with several paths are restored whole, and check paths are then relative to restic's full-path layout.
- restic stdin is left empty so it can never hang on a password prompt.
- restic errors are reduced to one short line. Exit codes 10 (missing repo) and 12 (wrong password) get fixed messages. Passwords embedded in repo URLs are redacted in messages.
- Tests create repos with `internal/testutil`, which skips when restic is not on PATH.
- `sqlite_integrity` opens the file through a `file:...?mode=ro` URI (works with spaces and Windows paths). A file SQLite cannot even open counts as a failure, like a non-"ok" integrity_check.
- A snapshot time in the future (clock skew) counts as age 0 for `max_age`.
- `min_files` counts regular files only (no directories or symlinks).
- Added an `internal/runner` package (not in the original layout) so the drill loop is testable with a fake backend, separately from the CLI.
- Two implicit checks are reported for every target: `snapshot` (latest snapshot found) and `restore`.
- When an earlier step fails, the checks that depend on it are reported as SKIP (they do not count as failures, but the target still fails). `max_age` still runs when only the restore failed, because it needs just the snapshot time.
- Temp dirs are deleted with a fallback that makes read-only restored files writable first. A failed cleanup becomes a warning in the report.
- Ctrl-C cancels the restic process through the context, so deferred cleanup still runs.
- State lives in `.<config name>.state.json` next to the config (`drill.yml` -> `.drill.state.json`), written atomically. A corrupt state file is a warning, not an error.
- Global flags may appear before or after the subcommand (`drill --json run` and `drill run --json` both work). `drill run` accepts several target names.
- `drill init` refuses to overwrite and returns exit 2. `drill list` always exits 0.
- Colour is used only when stdout is a terminal, and never when `NO_COLOR` is set or `TERM=dumb`.
- JSON output has `schema_version: 1`; `snapshot` is `null` when no snapshot was found.

## Known limitations

## What to do next
