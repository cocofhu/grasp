package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/workspacejob"
	"github.com/gin-gonic/gin"
)

func jobRouter(service *workspacejob.Service) *gin.Engine {
	r := gin.New()
	h := &WorkspaceJobs{Service: service}
	g := r.Group("/api/workspace/jobs")
	g.GET("/capabilities", h.Capabilities)
	g.POST("", h.Create)
	g.GET("/:jobId", h.Get)
	g.PATCH("/:jobId/fence", h.Renew)
	g.POST("/:jobId/cancel", h.Cancel)
	return r
}

func jobControlRequest(t *testing.T, client *http.Client, method, address string, input, output any, status int) {
	t.Helper()
	var data []byte
	if input != nil {
		var err error
		data, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), method, address, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != status {
		var failure any
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		t.Fatalf("control %s status=%d: %+v", method, resp.StatusCode, failure)
	}
	if output != nil {
		if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkspaceJobHandlersFailClosedAndBoundBodies(t *testing.T) {
	r := jobRouter(nil)
	for _, path := range []string{"/api/workspace/jobs/capabilities", "/api/workspace/jobs/job-0001"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("unconfigured runtime pretended readiness: %d", w.Code)
		}
	}
	s, err := workspacejob.New(workspacejob.Config{Root: t.TempDir(), Resource: "resource-one", Lineage: "lineage-original"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	r = jobRouter(s)
	for _, body := range []string{"{", strings.Repeat("x", 100000)} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/workspace/jobs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatal(w.Code)
		}
	}
}

// This real native operation and its HTTP heartbeats deliberately exceed the
// 45-second server lease. It is opt-in so regular unit tests remain quick.
func TestWorkspaceJobNativeLongControlProtocol(t *testing.T) {
	if os.Getenv("GRASP_WORKSPACE_JOB_NATIVE_LONG") != "1" {
		t.Skip("set GRASP_WORKSPACE_JOB_NATIVE_LONG=1 for the >45s native control proof")
	}
	started := time.Now()
	s, err := workspacejob.New(workspacejob.Config{Root: t.TempDir(), Resource: "resource-one", Lineage: "lineage-original", MaxLease: 8 * time.Second, MaxDuration: time.Minute, Executors: map[string]workspacejob.Executor{
		"control_probe": func(_ context.Context, h *workspacejob.Handle, _ json.RawMessage) (json.RawMessage, error) {
			if err := h.Command("", nil, nil, "/bin/sh", "-c", "sleep 48"); err != nil {
				return nil, err
			}
			return json.RawMessage(`{"nativeSeconds":48}`), h.WriteFile("retained-proof.txt", strings.NewReader("native control protocol completed"), 128)
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	server := httptest.NewServer(jobRouter(s))
	defer server.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	var caps workspacejob.Capabilities
	jobControlRequest(t, client, http.MethodGet, server.URL+"/api/workspace/jobs/capabilities", nil, &caps, http.StatusOK)
	if caps.FileCapture || caps.FileRestore || !caps.RenewableLease {
		t.Fatal("control test incorrectly advertised a file snapshot")
	}
	spec, hash, err := workspacejob.Fingerprint(workspacejob.Spec{ID: "native-control-job", Kind: "control_probe", Resource: caps.Resource, Lineage: caps.Lineage, Nonce: "native-control-nonce", Deadline: time.Now().Add(58 * time.Second), Body: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	fence := workspacejob.Fence{Owner: "native-control-owner", Generation: 17, ExpiresAt: time.Now().Add(7 * time.Second)}
	var receipt workspacejob.Receipt
	jobControlRequest(t, client, http.MethodPost, server.URL+"/api/workspace/jobs", workspacejob.CreateRequest{Spec: spec, SHA256: hash, Fence: fence}, &receipt, http.StatusAccepted)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	renewals := 0
	for {
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-ticker.C:
			jobControlRequest(t, client, http.MethodGet, server.URL+"/api/workspace/jobs/"+spec.ID, nil, &receipt, http.StatusOK)
			if receipt.State == "completed" {
				if time.Since(started) <= 45*time.Second || renewals < 20 || string(receipt.Result) != `{"nativeSeconds":48}` {
					t.Fatalf("insufficient long-running proof: %s %d %+v", time.Since(started), renewals, receipt)
				}
				t.Logf("real /bin/sh+sleep operation completed after %s with %d acknowledged HTTP renewals; no snapshot readiness claimed", time.Since(started).Round(time.Millisecond), renewals)
				return
			}
			if receipt.State != "running" && receipt.State != "accepted" {
				t.Fatal(fmt.Sprintf("native operation unexpectedly ended: %+v", receipt))
			}
			fence.ExpiresAt = time.Now().Add(7 * time.Second)
			if spec.Deadline.Before(fence.ExpiresAt) {
				fence.ExpiresAt = spec.Deadline
			}
			jobControlRequest(t, client, http.MethodPatch, server.URL+"/api/workspace/jobs/"+spec.ID+"/fence", workspacejob.RenewRequest{SHA256: hash, Lineage: spec.Lineage, Fence: fence}, &receipt, http.StatusOK)
			renewals++
		}
	}
}
