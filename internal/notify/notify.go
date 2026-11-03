// Package notify posts failure notifications to a webhook.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/enzog/drill/internal/report"
)

// Timeout bounds one webhook call.
const Timeout = 10 * time.Second

// Payload is the JSON body sent for a failed target. It never contains
// passwords: messages come from drill's checks and redacted restic errors.
type Payload struct {
	Tool         string        `json:"tool"`
	Target       string        `json:"target"`
	Status       string        `json:"status"`
	SnapshotID   string        `json:"snapshot_id,omitempty"`
	FailedChecks []FailedCheck `json:"failed_checks"`
}

// FailedCheck is one failed check in a payload.
type FailedCheck struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// PayloadFor builds the notification for a target result.
func PayloadFor(t report.Target) Payload {
	p := Payload{Tool: "drill", Target: t.Name, Status: string(t.Status), FailedChecks: []FailedCheck{}}
	if t.Snapshot != nil {
		p.SnapshotID = t.Snapshot.ShortID
	}
	for _, c := range t.Failed() {
		p.FailedChecks = append(p.FailedChecks, FailedCheck{Name: c.Name, Message: c.Message})
	}
	return p
}

// Send POSTs p as JSON to url. Any non-2xx response is an error.
func Send(ctx context.Context, client *http.Client, url string, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "drill")
	req.Header.Set("Title", "drill: "+p.Target+" failed") // shown by ntfy
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook: server answered %s", resp.Status)
	}
	return nil
}
