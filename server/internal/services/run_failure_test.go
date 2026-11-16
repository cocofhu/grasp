package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestAggregateRunFailureFromStateRun(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	db.Create(&models.Run{ID: "rf1", Status: "failed"})
	db.Create(&models.StateRun{RunID: "rf1", NodeID: "research", Status: "failed", Error: "sandbox setup failed: create timeout"})

	info := s.AggregateRunFailure("rf1")
	if info.Reason == "" || !strings.Contains(info.Reason, "sandbox setup failed") {
		t.Fatalf("reason=%q", info.Reason)
	}
	if info.FailedNode != "research" {
		t.Fatalf("failedNode=%q", info.FailedNode)
	}
	if !info.NoSandboxLog {
		t.Fatal("expected noSandboxLog when no archived logs")
	}
	display := info.DisplayReason()
	if !strings.Contains(display, "research") || !strings.Contains(display, NoSandboxLogMarker) {
		t.Fatalf("display=%q", display)
	}
	if info.SandboxLogNote == "" {
		t.Fatal("expected SandboxLogNote degrade explanation")
	}
}

func TestAggregateRunFailureLastErrorFallback(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	db.Create(&models.Run{ID: "rf2", Status: "failed"})
	db.Create(&models.RunVariable{RunID: "rf2", Name: "last_error", Type: "string", Value: "加载运行上下文失败: not found"})

	info := s.AggregateRunFailure("rf2")
	if info.Reason != "加载运行上下文失败: not found" {
		t.Fatalf("reason=%q", info.Reason)
	}
	if info.DisplayReason() == "" {
		t.Fatal("display empty")
	}
}

func TestAggregateRunFailureDefaultFallback(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	db.Create(&models.Run{ID: "rf3", Status: "failed"})
	info := s.AggregateRunFailure("rf3")
	if info.Reason != DefaultRunFailureReason {
		t.Fatalf("reason=%q", info.Reason)
	}
	if info.DisplayReason() == "" {
		t.Fatal("display empty")
	}
}

func TestAggregateRunFailureWithSandboxLog(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	db.Create(&models.Run{ID: "rf4", Status: "failed"})
	db.Create(&models.StateRun{RunID: "rf4", NodeID: "research", Status: "failed", Error: "agent exit 1"})
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "log-line-" + string(rune('A'+i%26))
	}
	db.Create(&models.SandboxLog{
		Name: "sbx-rf4", RunID: "rf4", NodeID: "research",
		Content:   strings.Join(lines, "\n") + "\nTAIL_MARKER",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	info := s.AggregateRunFailure("rf4")
	if info.NoSandboxLog {
		t.Fatal("expected sandbox log present")
	}
	if !strings.Contains(info.LogSummaryOrRef, "TAIL_MARKER") {
		t.Fatalf("log summary missing tail: %q", info.LogSummaryOrRef)
	}
	if strings.Contains(info.DisplayReason(), NoSandboxLogMarker) {
		t.Fatalf("display should not claim no log: %q", info.DisplayReason())
	}
}

func TestMarshalRunErrorJSON(t *testing.T) {
	body, err := MarshalRunErrorJSON(RunFailureInfo{
		Reason: "boom", FailedNode: "research", NoSandboxLog: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["failedNode"] != "research" {
		t.Fatalf("parsed=%v", parsed)
	}
	reason, _ := parsed["reason"].(string)
	if reason == "" || !strings.Contains(reason, "boom") {
		t.Fatalf("reason=%q", reason)
	}
	if parsed["noSandboxLog"] != true {
		t.Fatalf("noSandboxLog=%v", parsed["noSandboxLog"])
	}
}

func TestTruncateLogSummary(t *testing.T) {
	if TruncateLogSummary("") != "" {
		t.Fatal("empty")
	}
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line\n")
	}
	out := TruncateLogSummary(b.String())
	if strings.Count(out, "\n")+1 > maxLogSummaryLines {
		t.Fatalf("too many lines: %d", strings.Count(out, "\n")+1)
	}
}
