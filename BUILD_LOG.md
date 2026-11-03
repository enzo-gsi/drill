# drill build log

## Milestones
- [x] 1. Project skeleton, go.mod, config parsing and validation, with tests.
- [x] 2. restic backend: latest snapshot and restore to a temp dir, with integration tests on throwaway repos.
- [x] 3. Checks: max_age, exists, sqlite_integrity, min_files, each with tests.
- [x] 4. Runner, terminal and JSON reports, exit codes, state file, `drill list`.
- [x] 5. Webhook notification on failure, tested against httptest.Server.
- [x] 6. End-to-end failure tests (`cmd/drill/failures_test.go`): corrupted SQLite, missing file, old snapshot, recent snapshot only under another tag, no snapshots, wrong password, missing repo, config errors (exit 2). They also check that restore dirs are deleted, that no password appears in output, and that the repository is not modified.
- [x] 7. README, MIT LICENSE, GitHub Actions workflow (gofmt check, go vet, go test, demo), `scripts/demo.sh`.

Definition of done, checked on Windows 11 with Go 1.27.3 and restic 0.19.1: `gofmt -l .` prints nothing, `go vet ./...` and `go test ./...` pass (integration tests ran, none skipped), and `scripts/demo.sh` runs end to end (first drill passes, second fails with exit 1).

## Decisions
- Module path is `github.com/enzo-gsi/drill`.
- `go.mod` says `go 1.26.0`, not 1.22: current `modernc.org/sqlite` requires Go 1.26.
- YAML parsing uses `gopkg.in/yaml.v3` (the standard library has no YAML parser). Unknown fields are rejected so typos surface as errors.
- `max_age` accepts Go durations (`26h`, `90m`) plus a `d` suffix for days (`7d`).
- A relative `password_file` is resolved against the config file's directory; `~` is expanded.
- A target needs `password_file`, or `RESTIC_PASSWORD` / `RESTIC_PASSWORD_FILE` in the environment.
- A missing `password_file` is a config error (exit 2), not a check failure.
- Check paths must be relative and must not escape the restored tree (`..` and absolute paths are rejected).
- Config validation reports every problem at once, each prefixed with the target index and name.
- restic is always called with `--no-lock`, so drill never writes lock files into the repository (verified in tests).
- The latest snapshot is picked by time from `restic snapshots --json` (filtered by `--tag`), not with `--latest`, which groups by host and path.
- A snapshot with a single backup path is restored with restic's `<id>:<path>` subfolder syntax, so its contents land directly in the temp dir and check paths are relative to the backed-up directory. Snapshots with several paths are restored whole, and check paths are then relative to restic's full-path layout. (Restoring whole on Windows also fails on `C:\Users` timestamps, which the subfolder syntax avoids.)
- restic stdin is left empty so it can never hang on a password prompt.
- restic errors are reduced to one short line. Exit codes 10 (missing repo) and 12 (wrong password) get fixed messages. Passwords embedded in repo URLs are redacted in messages.
- Tests create repos with `internal/testutil`, which skips when restic is not on PATH.
- `sqlite_integrity` opens the file through a `file:...?mode=ro` URI (works with spaces and Windows paths). A file SQLite cannot even open counts as a failure, like a non-"ok" integrity_check. Integrity problems are shown on one line, at most 3, then "and N more problems".
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
- One webhook POST per failed target, with a 10s timeout. Payload: `tool`, `target`, `status`, `snapshot_id`, `failed_checks` (name + message). A `Title` header is set so ntfy shows a readable title.
- A webhook failure is printed as a warning and does not change the exit code (the drill result is what matters).
- The demo creates its SQLite file with a small Go helper (`scripts/mksqlite`) because the `sqlite3` CLI is not always installed. The helper also does the corruption.
- CI installs restic from the official GitHub release (0.19.1): Ubuntu's package is 0.16, which lacks the subfolder restore syntax and exit code 12.
- `.gitattributes` forces LF line endings so `gofmt` and `demo.sh` work after a checkout on Windows.

## Known limitations
- Needs restic 0.17 or newer (subfolder restore syntax, exit codes 10/12). drill does not check the restic version itself.
- Every run restores the full snapshot, so it needs as much free temp space as the snapshot size, and time to match. There is no partial restore of only the checked paths.
- The restore runs from the user's restic cache; drill does not run `restic check`, so repository-level corruption in packs that were not restored is not detected.
- Colour depends on stdout being a character device. In Git Bash/mintty on Windows the output is a pipe, so it shows no colour there.
- The CI workflow has not run yet (no remote repository).
- `exec.CommandContext` kills only restic itself on Ctrl-C; on Windows a restore can take a moment to stop.
- One target at a time; no parallelism.

## What to do next
1. Create the GitHub repository, update the module path in `go.mod`, imports and README if it is not `github.com/enzo-gsi/drill`, and check that CI passes.
2. Record the demo GIF with `bash scripts/demo.sh` (on Linux/macOS for colour) and add it to the README.
3. Try drill on a real but non-critical restic repository (sftp or rest-server) before pointing it at important backups.
4. Consider a restic version check with a clear error for versions older than 0.17.
5. Consider restoring only the paths the checks need (`restic restore --include`), for large snapshots.
6. Release binaries (for example with GoReleaser) so users do not need Go.
