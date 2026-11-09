package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/auth"
	"backend/internal/service"
	"backend/internal/workspacejob"
	"backend/internal/workspacesnapshot"
	"github.com/gin-gonic/gin"
)

func TestWorkspaceFilesAreAuthenticatedAndUseTheRealRuntimeController(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "uncommitted.txt"), []byte("retained working file"), 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := workspacesnapshot.NewController(workspacesnapshot.ControllerConfig{
		Engine:      workspacesnapshot.Config{Root: root},
		Jobs:        workspacejob.Config{Root: t.TempDir(), Resource: "resource-test", Lineage: "lineage-test"},
		RestoreRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controller.Close(context.Background()) })
	deps := &Dependencies{WebRoot: t.TempDir(), Chats: service.NewChatManager(), Workspace: controller}
	request := func(router *gin.Engine, method, path string, input any, cookie *http.Cookie, fence *workspacejob.RenewRequest) *httptest.ResponseRecorder {
		t.Helper()
		var data []byte
		if input != nil {
			data, err = json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if fence != nil {
			value, err := json.Marshal(fence)
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("X-Grasp-Workspace-Fence", base64.RawURLEncoding.EncodeToString(value))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := request(New(deps), http.MethodGet, "/api/workspace/files/capabilities", nil, nil, nil); w.Code != http.StatusNotFound {
		t.Fatal("standalone unauthenticated bridge exposed workspace operations", w.Code)
	}
	deps.Auth = auth.NewGuard("runtime-test-password")
	router := New(deps)
	if w := request(router, http.MethodGet, "/api/workspace/files/capabilities", nil, nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("workspace capabilities bypassed bridge authentication", w.Code)
	}
	login := request(router, http.MethodPost, "/api/login", map[string]string{"password": "runtime-test-password"}, nil, nil)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) == 0 {
		t.Fatal("runtime fixture login failed", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	caps := request(router, http.MethodGet, "/api/workspace/files/capabilities", nil, cookie, nil)
	var capability workspacesnapshot.FileCapabilities
	if caps.Code != http.StatusOK || json.Unmarshal(caps.Body.Bytes(), &capability) != nil || !capability.Capture || !capability.Read || !capability.Restore || capability.ProcessMemory {
		t.Fatal("router did not expose the complete controller capabilities", caps.Code)
	}
	var generation uint64
	newJob := func(id, kind string, body any) workspacejob.CreateRequest {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		spec, hash, err := workspacejob.Fingerprint(workspacejob.Spec{ID: id, Kind: kind, Resource: "resource-test", Lineage: "lineage-test", Nonce: "nonce-" + id, Deadline: time.Now().Add(10 * time.Second), Body: data})
		if err != nil {
			t.Fatal(err)
		}
		generation++
		return workspacejob.CreateRequest{Spec: spec, SHA256: hash, Fence: workspacejob.Fence{Owner: "owner-" + id, Generation: generation, ExpiresAt: time.Now().Add(3 * time.Second)}}
	}
	waitState := func(id, state string) workspacejob.Receipt {
		t.Helper()
		until := time.Now().Add(2 * time.Second)
		for {
			value, err := controller.Jobs.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if value.State == state {
				return value
			}
			if time.Now().After(until) || value.State == "failed" {
				t.Fatalf("runtime job did not reach %s: %+v", state, value)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	selection, err := controller.Engine.NewSpec([]string{"."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	capture := newJob("capture-test-0001", "capture_files", selection)
	if w := request(router, http.MethodPost, "/api/workspace/jobs", capture, cookie, nil); w.Code != http.StatusAccepted {
		t.Fatal("capture route did not dispatch the installed collector", w.Code, w.Body.String())
	}
	completed := waitState(capture.Spec.ID, "completed")
	var receipt workspacesnapshot.Receipt
	if err := json.Unmarshal(completed.Result, &receipt); err != nil {
		t.Fatal(err)
	}
	read := newJob("read-test-0001", "read_snapshot", workspacesnapshot.ReadSpec{SourceJobID: capture.Spec.ID, SourceSHA256: capture.SHA256, Receipt: receipt})
	if w := request(router, http.MethodPost, "/api/workspace/jobs", read, cookie, nil); w.Code != http.StatusAccepted {
		t.Fatal("read route failed", w.Code, w.Body.String())
	}
	waitState(read.Spec.ID, "running")
	fence := workspacejob.RenewRequest{SHA256: read.SHA256, Lineage: read.Spec.Lineage, Fence: read.Fence}
	fence.Fence.ExpiresAt = time.Now().Add(4 * time.Second)
	if w := request(router, http.MethodPatch, "/api/workspace/jobs/"+read.Spec.ID+"/fence", fence, cookie, nil); w.Code != http.StatusOK {
		t.Fatal("runtime renewal route differs from the server client", w.Code, w.Body.String())
	}
	path := "/api/workspace/jobs/" + read.Spec.ID + "/artifacts/archive?offset=0&length=64"
	if w := request(router, http.MethodGet, path, nil, cookie, nil); w.Code == http.StatusOK {
		t.Fatal("artifact read accepted no current owner fence")
	}
	if w := request(router, http.MethodGet, path, nil, cookie, &fence); w.Code != http.StatusOK || w.Body.Len() == 0 || w.Header().Get("X-Grasp-Snapshot-SHA256") == "" {
		t.Fatal("fenced artifact transport is not wired", w.Code, w.Body.String())
	}
	if w := request(router, http.MethodPost, "/api/workspace/jobs/"+read.Spec.ID+"/finish", nil, cookie, &fence); w.Code != http.StatusAccepted {
		t.Fatal("read completion is not wired", w.Code, w.Body.String())
	}
	waitState(read.Spec.ID, "completed")
}
