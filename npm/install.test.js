"use strict";

const test = require("node:test");
const assert = require("node:assert");
const { target, archiveName, expectedChecksum } = require("./install.js");

test("target maps Node platforms to release assets", () => {
  assert.deepStrictEqual(target("linux", "x64"), { goos: "linux", goarch: "amd64", ext: "tar.gz", exe: "drill" });
  assert.deepStrictEqual(target("win32", "arm64"), { goos: "windows", goarch: "arm64", ext: "zip", exe: "drill.exe" });
  assert.strictEqual(target("darwin", "arm64").goos, "darwin");
  assert.throws(() => target("aix", "x64"), /no prebuilt binary/);
  assert.throws(() => target("linux", "ia32"), /go install/);
});

test("archiveName matches the goreleaser name_template", () => {
  assert.strictEqual(archiveName("0.1.0", target("darwin", "arm64")), "drill_0.1.0_darwin_arm64.tar.gz");
  assert.strictEqual(archiveName("0.1.0", target("win32", "x64")), "drill_0.1.0_windows_amd64.zip");
});

test("expectedChecksum reads checksums.txt", () => {
  const sums = "aaa  drill_0.1.0_linux_amd64.tar.gz\nbbb  drill_0.1.0_windows_amd64.zip\n";
  assert.strictEqual(expectedChecksum(sums, "drill_0.1.0_windows_amd64.zip"), "bbb");
  assert.throws(() => expectedChecksum(sums, "other.zip"), /not listed/);
});
