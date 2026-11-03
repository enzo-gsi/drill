package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzo-gsi/drill/internal/checks"
	"github.com/enzo-gsi/drill/internal/report"
)

func TestPathFor(t *testing.T) {
	got := PathFor(filepath.Join("etc", "drill", "drill.yml"))
	if got != filepath.Join("etc", "drill", ".drill.state.json") {
		t.Errorf("got %s", got)
	}
	if got := PathFor("prod.yaml"); got != ".prod.state.json" {
		t.Errorf("got %s", got)
	}
}

func TestLoadMissing(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || s.Targets == nil || len(s.Targets) != 0 {
		t.Errorf("got %+v, %v", s, err)
	}
}

func TestLoadCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(p, []byte("{nope"), 0o644)
	s, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "corrupt") || s.Targets == nil {
		t.Errorf("got %+v, %v", s, err)
	}
	os.WriteFile(p, []byte("{}"), 0o644)
	if s, err := Load(p); err != nil || s.Targets == nil {
		t.Errorf("empty object: %+v, %v", s, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".drill.state.json")
	ran := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	var s State
	s.Record(report.Target{Name: "ok", Status: checks.Pass, StartedAt: ran, DurationMS: 5,
		Snapshot: &report.Snapshot{ID: "long", ShortID: "short", Time: ran.Add(-time.Hour)}})
	s.Record(report.Target{Name: "bad", Status: checks.Fail, StartedAt: ran, Checks: []report.Check{
		{Name: "snapshot", Status: checks.Fail}, {Name: "exists x", Status: checks.Skip},
	}})
	if err := Save(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	ok := got.Targets["ok"]
	if ok.Status != checks.Pass || ok.SnapshotID != "short" || !ok.RanAt.Equal(ran) || ok.DurationMS != 5 {
		t.Errorf("ok entry: %+v", ok)
	}
	bad := got.Targets["bad"]
	if bad.Status != checks.Fail || len(bad.Failed) != 1 || bad.Failed[0] != "snapshot" || bad.SnapshotID != "" {
		t.Errorf("bad entry: %+v", bad)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestSaveError(t *testing.T) {
	if err := Save(filepath.Join(t.TempDir(), "missing-dir", "s.json"), State{}); err == nil {
		t.Error("expected error writing into a missing directory")
	}
}

func TestRecordOverwrites(t *testing.T) {
	var s State
	s.Record(report.Target{Name: "a", Status: checks.Fail})
	s.Record(report.Target{Name: "a", Status: checks.Pass})
	if len(s.Targets) != 1 || s.Targets["a"].Status != checks.Pass {
		t.Errorf("got %+v", s.Targets)
	}
}
