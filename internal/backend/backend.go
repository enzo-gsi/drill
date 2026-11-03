// Package backend defines the interface drill uses to read backups.
package backend

import (
	"context"
	"time"
)

// Snapshot describes one backup snapshot.
type Snapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname,omitempty"`
	Paths    []string  `json:"paths,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
}

// Backend reads snapshots from a backup repository. Implementations must
// never modify the repository.
type Backend interface {
	// LatestSnapshot returns the most recent snapshot matching the target.
	LatestSnapshot(ctx context.Context) (Snapshot, error)
	// Restore restores snap into dest, an existing empty directory. The
	// contents of the backed-up directory end up directly under dest.
	Restore(ctx context.Context, snap Snapshot, dest string) error
}
