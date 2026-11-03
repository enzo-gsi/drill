package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzo-gsi/drill/internal/checks"
)

func sample() Report {
	start := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	r := Report{StartedAt: start, Targets: []Target{
		{Name: "ok", Status: checks.Pass, StartedAt: start, DurationMS: 1200,
			Snapshot: &Snapshot{ID: "abcdef123456", ShortID: "abcdef12", Time: start.Add(-time.Hour)},
			Checks:   []Check{{Name: "restore", Status: checks.Pass, Message: "restored", DurationMS: 900}}},
		{Name: "bad", Status: checks.Fail, StartedAt: start, DurationMS: 300,
			Checks: []Check{
				{Name: "snapshot", Status: checks.Fail, Message: "wrong password"},
				{Name: "exists a", Status: checks.Skip, Message: "not run"},
			},
			Warnings: []string{"could not delete temp dir"}},
	}}
	r.Finish(start.Add(2 * time.Second))
	return r
}

func TestFinish(t *testing.T) {
	r := sample()
	if r.Status != checks.Fail || r.DurationMS != 2000 || r.SchemaVersion != SchemaVersion {
		t.Errorf("unexpected report: %+v", r)
	}
	ok := Report{Targets: []Target{{Status: checks.Pass}}}
	ok.Finish(time.Now())
	if ok.Status != checks.Pass {
		t.Error("all-pass report should pass")
	}
}

func TestFailed(t *testing.T) {
	f := sample().Targets[1].Failed()
	if len(f) != 1 || f[0].Name != "snapshot" {
		t.Errorf("Failed() = %+v (skipped checks must not count)", f)
	}
}

func TestFromResult(t *testing.T) {
	c := FromResult(checks.Result{Name: "n", Status: checks.Pass, Message: "m", Duration: 1500 * time.Millisecond})
	if c != (Check{Name: "n", Status: checks.Pass, Message: "m", DurationMS: 1500}) {
		t.Errorf("got %+v", c)
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "status", "started_at", "duration_ms", "targets"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
	targets := got["targets"].([]any)
	first := targets[0].(map[string]any)
	snap := first["snapshot"].(map[string]any)
	if snap["id"] != "abcdef123456" || snap["short_id"] != "abcdef12" {
		t.Errorf("snapshot: %v", snap)
	}
	if targets[1].(map[string]any)["snapshot"] != nil {
		t.Error("missing snapshot must be null")
	}

	buf.Reset()
	if err := WriteJSON(&buf, Report{}); err != nil || !strings.Contains(buf.String(), `"targets": []`) {
		t.Errorf("empty report should have an empty targets array: %s", buf.String())
	}
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, sample(), false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"ok  snapshot abcdef12 from",
		"PASS  restore",
		"ok: PASS, 1 checks passed",
		"FAIL  snapshot",
		"SKIP  exists a",
		"warning: could not delete temp dir",
		"bad: FAIL, 1 of 2 checks failed",
		"2 targets: 1 passed, 1 failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("colour codes without colour enabled")
	}
	buf.Reset()
	WriteText(&buf, sample(), true)
	if !strings.Contains(buf.String(), red+"FAIL"+reset) {
		t.Error("colour enabled but FAIL is not red")
	}
}

func TestStatusLabel(t *testing.T) {
	plain := func(_, s string) string { return s }
	if statusLabel(checks.Pass, plain) != "PASS" || statusLabel(checks.Fail, plain) != "FAIL" || statusLabel(checks.Skip, plain) != "SKIP" {
		t.Error("wrong labels")
	}
}
