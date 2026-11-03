#!/usr/bin/env bash
# Demo for the README GIF: back up a healthy app, drill it (pass), corrupt its
# database, back up again, drill again (fail). Everything lives in a temp dir.
set -euo pipefail

cd "$(dirname "$0")/.."
command -v restic >/dev/null || { echo "demo.sh needs restic on PATH" >&2; exit 2; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Native path for config files (Git Bash on Windows needs C:/... paths).
native() { if command -v cygpath >/dev/null; then cygpath -m "$1"; else printf '%s\n' "$1"; fi; }

drill="$work/drill$(go env GOEXE)"
go build -o "$drill" ./cmd/drill
go build -o "$work/mksqlite$(go env GOEXE)" ./scripts/mksqlite

say() { printf '\n\033[1;36m# %s\033[0m\n' "$*"; sleep "${DEMO_PAUSE:-1}"; }
show() { printf '\033[1m$ %s\033[0m\n' "$*"; }

app="$work/vaultwarden"
repo="$work/repo"
mkdir -p "$app/data/attachments"
"$work/mksqlite" "$app/data/db.sqlite3"
for i in $(seq 1 12); do echo "attachment $i" > "$app/data/attachments/file-$i.bin"; done
echo "drill-demo-password" > "$work/restic-pass"

restic init --repo "$repo" --password-file "$work/restic-pass" --quiet
cat > "$work/drill.yml" <<EOF
targets:
  - name: vaultwarden
    backend: restic
    repo: $(native "$repo")
    password_file: $(native "$work/restic-pass")
    tag: vaultwarden
    checks:
      - max_age: 26h
      - exists: data/db.sqlite3
      - sqlite_integrity: data/db.sqlite3
      - min_files: 10
EOF

backup() {
  show "restic backup --tag vaultwarden ./vaultwarden"
  restic backup --repo "$repo" --password-file "$work/restic-pass" --tag vaultwarden --quiet "$app"
  echo "snapshot saved"
}

say "Nightly backup of Vaultwarden. restic says success."
backup

say "Prove it: restore the latest snapshot and check it."
show "drill run"
"$drill" run --config "$work/drill.yml"

say "Disaster: the database gets corrupted, and backups keep running..."
"$work/mksqlite" -corrupt "$app/data/db.sqlite3"
backup

say "restic still says success. drill does not."
show "drill run"
set +e
"$drill" run --config "$work/drill.yml"
code=$?
set -e
printf '\nexit code %d\n' "$code"

if [ "$code" -ne 1 ]; then
  echo "demo.sh: expected drill to fail with exit code 1, got $code" >&2
  exit 1
fi
