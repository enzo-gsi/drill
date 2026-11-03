# drill build log

## Milestones
- [x] 1. Project skeleton, go.mod, config parsing and validation, with tests.

## Decisions
- Module path is `github.com/enzog/drill` (placeholder until the real GitHub repo exists).
- YAML parsing uses `gopkg.in/yaml.v3` (the standard library has no YAML parser). Unknown fields are rejected so typos surface as errors.
- `max_age` accepts Go durations (`26h`, `90m`) plus a `d` suffix for days (`7d`).
- A relative `password_file` is resolved against the config file's directory; `~` is expanded.
- A target needs `password_file`, or `RESTIC_PASSWORD` / `RESTIC_PASSWORD_FILE` in the environment.
- Check paths must be relative and must not escape the restored tree (`..` and absolute paths are rejected).
- Config validation reports every problem at once, each prefixed with the target index and name.

## Known limitations

## What to do next
