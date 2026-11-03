#!/usr/bin/env node
// Runs the drill binary, downloading it first if postinstall was skipped
// (for example with npm --ignore-scripts).
"use strict";

const fs = require("node:fs");
const { spawnSync } = require("node:child_process");
const { binaryPath, install } = require("../install.js");

async function main() {
  const bin = binaryPath();
  if (!fs.existsSync(bin)) await install();
  const res = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
  if (res.error) throw res.error;
  process.exit(res.status === null ? 1 : res.status);
}

main().catch((err) => {
  console.error(`drill-restic: ${err.message}`);
  process.exit(2);
});
