package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloCloseAllowsProcessExitBeyondTenSeconds(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, _ := newSoloFixture(t, port, 1)
	view, err := solo.Create(t.Context(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resident ready", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	before, _ := solo.Require(view.ID, "alice")
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseStop := func() { releaseOnce.Do(func() { close(release) }) }
	interceptStopGateway(t, sbx, gateway.Server, func(r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
	})
	t.Cleanup(releaseStop)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, failure := solo.Close(ctx, view.ID, "alice"); done <- failure }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("stop request never reached the gateway")
	}
	select {
	case failure := <-done:
		t.Fatalf("normal process termination was cut off before confirmation: %v", failure)
	case <-time.After(10*time.Second + 100*time.Millisecond):
	}
	current, _ := solo.Require(view.ID, "alice")
	if current.Status != models.SoloStatusClosed || current.SandboxID != before.SandboxID || sbx.activeSoloSandboxCount() != 1 {
		t.Fatalf("pending stop discarded its binding or released compute: %+v", current)
	}
	releaseStop()
	select {
	case failure := <-done:
		if failure != nil {
			t.Fatal(failure)
		}
	case <-ctx.Done():
		t.Fatal("confirmed stop did not complete")
	}
	current, _ = solo.Require(view.ID, "alice")
	if current.SandboxID != before.SandboxID || current.LifecycleAction != "" || sbx.activeSoloSandboxCount() != 0 || !gateway.Has(beforeName(t, solo, before.SandboxID)) {
		t.Fatalf("confirmed stop did not retain its workspace and release compute: %+v", current)
	}
}

func beforeName(t *testing.T, solo *SoloService, sandboxID uint) string {
	t.Helper()
	var row models.Sandbox
	if err := solo.db.First(&row, sandboxID).Error; err != nil {
		t.Fatal(err)
	}
	return row.Name
}

func TestSoloStopRecoveryConfirmsLostAcknowledgementWithoutRepostingStop(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, _ := newSoloFixture(t, port, 1)
	view, err := solo.Create(t.Context(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resident ready", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	before, _ := solo.Require(view.ID, "alice")
	var stopCalls atomic.Int32
	interceptStopGateway(t, sbx, gateway.Server, func(r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			stopCalls.Add(1)
		}
	})
	gateway.FailStop = true
	_, err = solo.Close(t.Context(), view.ID, "alice")
	requireSoloFailure(t, err, "environment_stop_pending")
	gateway.SetStatus(beforeName(t, solo, before.SandboxID), "stopped")
	sbx.RecoverSoloSandboxes(t.Context())
	current, _ := solo.Require(view.ID, "alice")
	if stopCalls.Load() != 1 || current.LifecycleAction != "" || sbx.activeSoloSandboxCount() != 0 || current.SandboxID != before.SandboxID {
		t.Fatalf("lost acknowledgement was not reconciled from physical state: calls=%d current=%+v", stopCalls.Load(), current)
	}
}

func TestSoloUnconfirmedStopKeepsDiagnosticsOutOfUserState(t *testing.T) {
	for _, closeChat := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "close"}[closeChat], func(t *testing.T) {
			acp, port := fakeACPServer(t)
			defer acp.Close()
			solo, _, gateway, _ := newSoloFixture(t, port, 1)
			view, err := solo.Create(t.Context(), "alice", SoloCreateInput{AgentName: "agentA"})
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "resident ready", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
			gateway.FailStop = true
			if closeChat {
				_, err = solo.Close(t.Context(), view.ID, "alice")
			} else {
				_, err = solo.StopEnvironment(t.Context(), view.ID, "alice")
			}
			var failure *SoloError
			if !errors.As(err, &failure) || failure.Code != "environment_stop_pending" {
				t.Fatalf("stop failure has no safe typed response: %v", err)
			}
			state, err := solo.Get(t.Context(), view.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(state)
			for _, text := range []string{failure.Msg, string(body)} {
				for _, internal := range []string{"http://", "POST /api/", "gateway POST", "stop failed"} {
					if strings.Contains(text, internal) {
						t.Fatalf("internal stop details escaped into user state: %s", text)
					}
				}
			}
		})
	}
}
