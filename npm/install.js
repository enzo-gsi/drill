#!/usr/bin/env node
// Downloads the drill binary matching this package version from the GitHub
// release, checks its SHA-256 against checksums.txt, and extracts it to bin/.
"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const REPO = "enzo-gsi/drill";
const BIN_DIR = path.join(__dirname, "bin");

const GOOS = { linux: "linux", darwin: "darwin", win32: "windows" };
const GOARCH = { x64: "amd64", arm64: "arm64" };

function target(platform = process.platform, arch = process.arch) {
  const goos = GOOS[platform];
  const goarch = GOARCH[arch];
  if (!goos || !goarch) {
    throw new Error(`drill has no prebuilt binary for ${platform}/${arch}. Install it with: go install github.com/${REPO}/cmd/drill@latest`);
  }
  return { goos, goarch, ext: goos === "windows" ? "zip" : "tar.gz", exe: goos === "windows" ? "drill.exe" : "drill" };
}

function archiveName(version, t) {
  return `drill_${version}_${t.goos}_${t.goarch}.${t.ext}`;
}

function binaryPath(t = target()) {
  return path.join(BIN_DIR, t.exe);
}

// expectedChecksum finds the hash for name in a goreleaser checksums.txt.
function expectedChecksum(checksums, name) {
  for (const line of checksums.split("\n")) {
    const [hash, file] = line.trim().split(/\s+/);
    if (file === name) return hash;
  }
  throw new Error(`${name} is not listed in checksums.txt`);
}

async function download(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`download failed: ${url} answered ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

async function install() {
  const version = require("./package.json").version;
  const t = target();
  const name = archiveName(version, t);
  const base = `https://github.com/${REPO}/releases/download/v${version}`;

  const [archive, checksums] = await Promise.all([download(`${base}/${name}`), download(`${base}/checksums.txt`)]);
  const actual = crypto.createHash("sha256").update(archive).digest("hex");
  const expected = expectedChecksum(checksums.toString("utf8"), name);
  if (actual !== expected) throw new Error(`checksum mismatch for ${name}`);

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "drill-npm-"));
  try {
    const file = path.join(tmp, name);
    fs.writeFileSync(file, archive);
    // bsdtar (macOS, Windows 10+) and GNU tar both handle .tar.gz; bsdtar also handles .zip.
    execFileSync("tar", ["-xf", file, "-C", tmp], { stdio: "ignore" });
    fs.mkdirSync(BIN_DIR, { recursive: true });
    fs.copyFileSync(path.join(tmp, t.exe), binaryPath(t));
    fs.chmodSync(binaryPath(t), 0o755);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

module.exports = { target, archiveName, binaryPath, expectedChecksum, install };

if (require.main === module) {
  install().catch((err) => {
    console.error(`drill-restic: ${err.message}`);
    process.exit(1);
  });
}
