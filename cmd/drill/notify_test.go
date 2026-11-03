package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/enzog/drill/internal/notify"
	"github.com/enzog/drill/internal/testutil"
)

type webhookRecorder struct {
	mu       sync.Mutex
	payloads []notify.Payload
	raw      []string
}

func newWebhook(t *testing.T, status int) (*webhookRecorder, string) {
	rec := &webhookRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p notify.Payload
		json.Unmarshal(body, &p)
		rec.mu.Lock()
		rec.payloads = append(rec.payloads, p)
		rec.raw = append(rec.raw, string(body))
		rec.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

func addWebhook(t *testing.T, configPath, url string) {
	t.Helper()
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString("notify:\n  webhook: " + url + "\n")
}

func TestWebhookOnFailure(t *testing.T) {
	f := newFixture(t, "      - exists: data/missing.txt\n")
	f.repo.Backup(t, f.src, "--tag", "app")
	rec, url := newWebhook(t, http.StatusOK)
	addWebhook(t, f.config, url)

	r := drill(t, "run", "--config", f.config)
	if r.code != exitFailed {
		t.Fatalf("exit %d: %s %s", r.code, r.stdout, r.stderr)
	}
	if len(rec.payloads) != 1 {
		t.Fatalf("expected one webhook call, got %d", len(rec.payloads))
	}
	p := rec.payloads[0]
	if p.Target != "app" || p.SnapshotID == "" || len(p.FailedChecks) != 1 || p.FailedChecks[0].Name != "exists data/missing.txt" {
		t.Errorf("payload: %+v", p)
	}
	if strings.Contains(rec.raw[0], testutil.Password) || strings.Contains(rec.raw[0], f.repo.PasswordFile) {
		t.Error("webhook payload leaks a secret")
	}
}

func TestNoWebhookOnPass(t *testing.T) {
	f := newFixture(t, "      - exists: data/notes.txt\n")
	f.repo.Backup(t, f.src, "--tag", "app")
	rec, url := newWebhook(t, http.StatusOK)
	addWebhook(t, f.config, url)
	if r := drill(t, "run", "--config", f.config); r.code != exitOK {
		t.Fatalf("exit %d: %s %s", r.code, r.stdout, r.stderr)
	}
	if len(rec.payloads) != 0 {
		t.Errorf("webhook called on success: %v", rec.raw)
	}
}

func TestWebhookErrorIsWarning(t *testing.T) {
	f := newFixture(t, "      - exists: data/missing.txt\n")
	f.repo.Backup(t, f.src, "--tag", "app")
	_, url := newWebhook(t, http.StatusInternalServerError)
	addWebhook(t, f.config, url)
	r := drill(t, "run", "--config", f.config)
	if r.code != exitFailed || !strings.Contains(r.stderr, "warning: notifying failure of app") {
		t.Errorf("got %+v", r)
	}
}
