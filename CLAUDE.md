# drill

Go CLI that proves restic backups actually restore.
Tagline: "Your backups say 'success'. Prove it."

## Safety rules (never break these)
- NEVER write to, forget from, or prune a user's backup repository. drill only reads (`restic snapshots`, `restic restore`).
  Tests may create and modify throwaway repos in t.TempDir() only.
- Always restore into a temp dir and delete it afterwards, even on error (defer).
- Never print passwords or password file contents in logs, errors or JSON output.
- Call restic with an argument slice (exec.Command), never through a shell string.
- Never run `git push`, never create remote repos, never touch files outside this project directory.

## Conventions
- Code, comments, README and commit messages in English.
- Standard library first. Pure Go only (no CGO): use modernc.org/sqlite for SQLite checks.
- Every function gets a test. Run `gofmt`, `go vet ./...` and `go test ./...` before each commit.
- Small commits, one per milestone, message format: `feat: ...`, `test: ...`, `docs: ...`.

## Out of scope for v0.1
Borg, Docker volumes, compose health checks, web UI, scheduling daemon.
