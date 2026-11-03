// Package state remembers the last result of each target between runs.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enzog/drill/internal/checks"
	"github.com/enzog/drill/internal/report"
)

// State is the content of the state file.
type State struct {
	Targets map[string]Entry `json:"targets"`
}

// Entry is the last result of one target.
type Entry struct {
	Status       checks.Status `json:"status"`
	RanAt        time.Time     `json:"ran_at"`
	DurationMS   int64         `json:"duration_ms"`
	SnapshotID   string        `json:"snapshot_id,omitempty"`
	SnapshotTime time.Time     `json:"snapshot_time,omitzero"`
	Failed       []string      `json:"failed_checks,omitempty"`
}

// PathFor returns the state file used for a config file: drill.yml ->
// .drill.state.json in the same directory.
func PathFor(configPath string) string {
	base := filepath.Base(configPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(filepath.Dir(configPath), "."+base+".state.json")
}

// Load reads a state file. A missing file is an empty state.
func Load(path string) (State, error) {
	s := State{Targets: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("reading state: %w", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return State{Targets: map[string]Entry{}}, fmt.Errorf("state file %s is corrupt: %v", path, err)
	}
	if s.Targets == nil {
		s.Targets = map[string]Entry{}
	}
	return s, nil
}

// Save writes the state file atomically.
func Save(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".drill-state-*.tmp")
	if err != nil {
		return fmt.Errorf("writing state: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}
	return nil
}

// Record stores the result of a target run.
func (s *State) Record(t report.Target) {
	if s.Targets == nil {
		s.Targets = map[string]Entry{}
	}
	e := Entry{Status: t.Status, RanAt: t.StartedAt, DurationMS: t.DurationMS}
	if t.Snapshot != nil {
		e.SnapshotID, e.SnapshotTime = t.Snapshot.ShortID, t.Snapshot.Time
	}
	for _, c := range t.Failed() {
		e.Failed = append(e.Failed, c.Name)
	}
	s.Targets[t.Name] = e
}
