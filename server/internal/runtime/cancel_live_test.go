package runtime

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
)

// TestCancelAbortsLiveAgent verifies AbortRun tears down an in-flight live
// sandbox agent so a follow-up RunAgent can admit. Complements the engine
// cancel/resume unit suite with the real universal-sandbox + Cursor path.
//
//	GRASP_LIVE_CANCEL=1 GRASP_CURSOR_API_KEY=crsr_… \
//	GRASP_SANDBOX_IMAGE=universal-sandbox:local \
//	GRASP_SANDBOX_GATEWAY_URL=http://127.0.0.1:8899 \
//	go test ./internal/runtime/ -run TestCancelAbortsLiveAgent -v -timeout 30m
func TestCancelAbortsLiveAgent(t *testing.T) {
	if os.Getenv("GRASP_LIVE_CANCEL") != "1" {
		t.Skip("set GRASP_LIVE_CANCEL=1 (and GRASP_CURSOR_API_KEY) to run")
	}
	apiKey := os.Getenv("GRASP_CURSOR_API_KEY")
	if apiKey == "" {
		t.Fatal("GRASP_CURSOR_API_KEY required")
	}
	image := getenvOr("GRASP_SANDBOX_IMAGE", "universal-sandbox:local")
	gatewayURL := getenvOr("GRASP_SANDBOX_GATEWAY_URL", "http://127.0.0.1:8899")

	store := newMemStore()
	host := mcp.NewHost(store)
	runID := "live-cancel-1"
	token := host.RegisterRun(runID)
	defer host.UnregisterRun(runID)

	keyJSON, _ := json.Marshal(apiKey)
	profiles := writeAgent(t, "backend-dev", `{"acpBackend":"cursor","env":{"GRASP_CURSOR_API_KEY":`+string(keyJSON)+`}}`)
	provider := newACPProvider(host, Options{
		SandboxImage: image,
		GatewayURL:   gatewayURL,
		ProfilesRoot: profiles,
		ChatTimeout:  8 * time.Minute,
	})
	aborter, ok := provider.(RunAborter)
	if !ok {
		t.Fatal("provider does not implement RunAborter")
	}

	req := NodeReq{
		RunID:    runID,
		Token:    token,
		NodeID:   "work",
		NodeType: "agent", Caps: testPlainCaps,
		Config: map[string]any{
			"agent_profile": "backend-dev",
			// Keep the agent busy long enough for AbortRun to race mid-turn.
			"prompt": "请先等待并思考 3 分钟，期间不要创建任何文件；然后在工作目录创建 wait.txt，内容为 done。",
		},
		Vars: map[string]any{},
	}

	var (
		wg     sync.WaitGroup
		runErr error
	)
	wg.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	go func() {
		defer wg.Done()
		_, runErr = provider.RunAgent(ctx, req)
	}()

	// Allow gateway create + ACP session start before aborting.
	time.Sleep(45 * time.Second)
	aborter.AbortRun(runID)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Minute):
		t.Fatal("RunAgent did not return after AbortRun")
	}
	if runErr == nil {
		t.Log("warning: RunAgent returned nil error after abort (acceptable if agent finished early)")
	} else {
		t.Logf("RunAgent after abort: %v", runErr)
	}

	// Fresh visit must still be able to run (no sticky abort at provider).
	runID2 := "live-cancel-2"
	tok2 := host.RegisterRun(runID2)
	defer host.UnregisterRun(runID2)
	req2 := req
	req2.RunID = runID2
	req2.Token = tok2
	req2.Config = map[string]any{
		"agent_profile": "backend-dev",
		"prompt":        "在工作目录创建文件 ok.txt，内容仅一行：ok。不要创建其他文件。",
		"produces":      "ok.txt",
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel2()
	res, err := provider.RunAgent(ctx2, req2)
	if err != nil {
		t.Fatalf("second RunAgent after abort: %v", err)
	}
	if res.OutputMd == "" {
		t.Error("expected non-empty narration on second run")
	}
	if content, ok := store.Get(runID2, "ok.txt"); !ok || len(content) == 0 {
		t.Fatalf("produces ok.txt missing after second run (ok=%v len=%d)", ok, len(content))
	}
}
