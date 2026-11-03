package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/enzog/drill/internal/checks"
	"github.com/enzog/drill/internal/report"
)

func failedTarget() report.Target {
	return report.Target{
		Name: "vw", Status: checks.Fail,
		Snapshot: &report.Snapshot{ID: "long", ShortID: "short", Time: time.Now()},
		Checks: []report.Check{
			{Name: "restore", Status: checks.Pass},
			{Name: "sqlite_integrity db", Status: checks.Fail, Message: "db failed integrity_check"},
			{Name: "exists x", Status: checks.Skip},
		},
	}
}

func TestPayloadFor(t *testing.T) {
	p := PayloadFor(failedTarget())
	if p.Tool != "drill" || p.Target != "vw" || p.Status != "fail" || p.SnapshotID != "short" {
		t.Errorf("got %+v", p)
	}
	if len(p.FailedChecks) != 1 || p.FailedChecks[0].Name != "sqlite_integrity db" {
		t.Errorf("failed checks: %+v", p.FailedChecks)
	}
	noSnap := PayloadFor(report.Target{Name: "x", Status: checks.Fail})
	if noSnap.SnapshotID != "" || noSnap.FailedChecks == nil {
		t.Errorf("no snapshot: %+v", noSnap)
	}
}

func TestSend(t *testing.T) {
	var gotBody []byte
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()

	if err := Send(context.Background(), srv.Client(), srv.URL+"/topic", PayloadFor(failedTarget())); err != nil {
		t.Fatal(err)
	}
	if gotReq.Method != http.MethodPost || gotReq.URL.Path != "/topic" || gotReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("request: %s %s %v", gotReq.Method, gotReq.URL, gotReq.Header)
	}
	if !strings.Contains(gotReq.Header.Get("Title"), "vw failed") {
		t.Errorf("title header: %q", gotReq.Header.Get("Title"))
	}
	var p Payload
	if err := json.Unmarshal(gotBody, &p); err != nil || p.Target != "vw" || len(p.FailedChecks) != 1 {
		t.Errorf("body %s: %v", gotBody, err)
	}
}

func TestSendErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	err := Send(context.Background(), srv.Client(), srv.URL, Payload{Target: "x"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("non-2xx: %v", err)
	}

	srv.Close()
	if err := Send(context.Background(), srv.Client(), srv.URL, Payload{}); err == nil {
		t.Error("closed server should fail")
	}
	if err := Send(context.Background(), http.DefaultClient, "::bad url", Payload{}); err == nil {
		t.Error("bad URL should fail")
	}
}
