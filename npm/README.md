# drill-restic

**Your backups say 'success'. Prove it.**

npm wrapper for [drill](https://github.com/enzo-gsi/drill), a CLI that restores your latest restic snapshot into a temp dir and checks it (age, files, SQLite integrity).

```bash
npm install -g drill-restic
drill init
drill run
```

Or without installing: `npx drill-restic run`.

On install, the matching prebuilt binary is downloaded from the GitHub release and verified against its SHA-256 checksum. restic 0.17+ must be on your `PATH`.

Full docs: https://github.com/enzo-gsi/drill
