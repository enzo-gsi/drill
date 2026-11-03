# drill

**Your backups say 'success'. Prove it.**

drill restores the latest [restic](https://restic.net) snapshot into a temp dir, runs checks on what came back, and tells you, with an exit code, whether you could actually recover from it. It catches the backups that "succeed" every night while quietly saving a corrupted database, an empty folder, or nothing at all since March.

<!-- Demo GIF: record `bash scripts/demo.sh` and put it here. -->

## Install

```bash
go install github.com/enzo-gsi/drill/cmd/drill@latest
```

Requires Go 1.26+ to build and restic 0.17+ on `PATH`.

## Quickstart

```bash
drill init          # writes an example drill.yml
$EDITOR drill.yml   # point it at your repository
drill run           # restore, check, report
```

```
vaultwarden  snapshot 359a12c0 from 2026-11-03 18:09
  PASS  snapshot                          latest snapshot 359a12c0 from 2026-11-03 18:09
  PASS  restore                           restored into a temp dir in 600ms
  PASS  max_age 26h                       latest snapshot is 1s old (limit 26h)
  PASS  exists data/db.sqlite3            data/db.sqlite3 exists (500.0 KB)
  FAIL  sqlite_integrity data/db.sqlite3  data/db.sqlite3 failed integrity_check: Tree 2 page 64: ...
  PASS  min_files 10                      restored 13 files (minimum 10)
vaultwarden: FAIL, 1 of 6 checks failed in 1.2s
```

Run it from cron or a systemd timer after your backup job, and get a webhook when it fails:

```cron
30 4 * * *  drill run --config /etc/drill/drill.yml
```

## Commands

| Command | What it does |
|---|---|
| `drill init` | Write an example `drill.yml` in the current directory. Never overwrites. |
| `drill run [target...]` | Drill the named targets, or all of them. |
| `drill list` | Show each target and the result of its last run. |

Global flags, accepted anywhere on the command line:

- `--config path`: config file (default `./drill.yml`)
- `--json`: machine-readable output

Exit codes:

| Code | Meaning |
|---|---|
| 0 | Every check passed |
| 1 | At least one check failed (including "could not find or restore a snapshot") |
| 2 | Config or usage error |

## Config reference

```yaml
targets:
  - name: vaultwarden                 # required, no spaces or slashes
    backend: restic                   # required, only restic for now
    repo: sftp:user@host:/srv/restic  # required, any restic repository string
    password_file: ~/.restic-pass     # or set RESTIC_PASSWORD / RESTIC_PASSWORD_FILE
    tag: vaultwarden                  # optional, only consider snapshots with this tag
    checks:
      - max_age: 26h
      - exists: data/db.sqlite3
      - sqlite_integrity: data/db.sqlite3
      - min_files: 10
notify:
  webhook: https://ntfy.sh/my-topic   # optional, POSTed when a target fails
```

A relative `password_file` is resolved against the config file's directory. Any other restic setting (for example `AWS_ACCESS_KEY_ID` for S3) is read from the environment by restic as usual.

### Checks

Every target gets two implicit checks first: `snapshot` (a matching snapshot exists) and `restore` (it restores without error). Then:

| Check | Passes when |
|---|---|
| `max_age: 26h` | The latest matching snapshot is younger than the duration. Units: `s`, `m`, `h`, `d` (e.g. `90m`, `7d`). |
| `exists: path` | The path exists in the restored tree. |
| `sqlite_integrity: path` | The file opens read-only as SQLite and `PRAGMA integrity_check` returns `ok`. |
| `min_files: N` | The restored tree holds at least N regular files. |

Paths are relative to the directory you backed up. For `restic backup /srv/vaultwarden`, `data/db.sqlite3` means `/srv/vaultwarden/data/db.sqlite3`. If a snapshot contains several backup paths, paths are relative to restic's full restore layout (`srv/vaultwarden/data/db.sqlite3`).

When the snapshot or restore fails, checks that depend on it are shown as `SKIP`.

### JSON output

`drill run --json` prints a stable schema (`schema_version: 1`):

```json
{
  "schema_version": 1,
  "status": "fail",
  "started_at": "2026-11-03T18:09:12Z",
  "duration_ms": 1210,
  "targets": [
    {
      "name": "vaultwarden",
      "status": "fail",
      "started_at": "2026-11-03T18:09:12Z",
      "duration_ms": 1210,
      "snapshot": { "id": "359a12c0...", "short_id": "359a12c0", "time": "2026-11-03T18:09:10Z" },
      "checks": [
        { "name": "restore", "status": "pass", "message": "restored into a temp dir in 600ms", "duration_ms": 600 },
        { "name": "sqlite_integrity data/db.sqlite3", "status": "fail", "message": "...", "duration_ms": 25 }
      ]
    }
  ]
}
```

`status` is `pass`, `fail` or `skip`. `snapshot` is `null` when none was found.

### Webhook

On failure drill POSTs one JSON body per failed target:

```json
{ "tool": "drill", "target": "vaultwarden", "status": "fail", "snapshot_id": "359a12c0",
  "failed_checks": [{ "name": "sqlite_integrity data/db.sqlite3", "message": "..." }] }
```

It works with ntfy, or anything that accepts a POST. A webhook error is printed as a warning and does not change the exit code.

### State

`drill run` records each target's last result in `.drill.state.json` next to the config (named after the config file). `drill list` reads it.

## Safety guarantees

- **Read-only.** drill only runs `restic snapshots` and `restic restore`, always with `--no-lock`, so it never writes to, forgets from, or prunes your repository, not even a lock file. The test suite checks that the repository is unchanged after a drill.
- **Temp dirs are always deleted**, even when a check fails, restic errors, or you press Ctrl-C.
- **No secrets in output.** Passwords never appear in logs, errors, JSON or webhooks; passwords embedded in repository URLs are redacted.
- **No shell.** restic is called with an argument list, never through a shell string.
- **Never hangs on a prompt.** restic gets no stdin, so a missing password fails fast instead of waiting.

Restores need free disk space for a full copy of the snapshot in your temp dir. Set `TMPDIR` (`TMP` on Windows) to put it somewhere else.

## Development

```bash
go test ./...            # integration tests need restic on PATH, otherwise they skip
bash scripts/demo.sh     # healthy backup passes, corrupted one fails
```

## License

MIT
