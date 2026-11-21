package provider

import (
	"log"
	"os"
	"strings"
	"testing"
)

func TestOutputTailKeepsTheEnd(t *testing.T) {
	tail := &OutputTail{Max: 8, LogPrefix: "test"}
	_, _ = tail.Write([]byte("first line\n"))
	_, _ = tail.Write([]byte("  last\n"))
	if got := tail.String(); got != "last" {
		t.Fatalf("tail %q", got)
	}
}

func TestOutputTailRedactsMirroredLines(t *testing.T) {
	var logged strings.Builder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	tail := &OutputTail{Max: 1024, LogPrefix: "child", Env: []string{"OPENCODE_SERVER_PASSWORD=s3cr3t-pw"}}
	_, _ = tail.Write([]byte("auth failed for s3cr3t-pw\nAuthorization: Bearer abc.def\n"))
	if strings.Contains(logged.String(), "s3cr3t-pw") || strings.Contains(logged.String(), "abc.def") {
		t.Fatalf("log leaked a credential: %q", logged.String())
	}
	if !strings.Contains(logged.String(), "child: auth failed for [redacted]") {
		t.Fatalf("log = %q", logged.String())
	}
}

func TestOutputTailRedactsSecretsSplitAcrossWritesAndCutLines(t *testing.T) {
	var logged strings.Builder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	tail := &OutputTail{Max: 4096, LogPrefix: "child", Env: []string{"OPENCODE_SERVER_PASSWORD=s3cr3t-pw"}}
	_, _ = tail.Write([]byte("auth failed for s3cr"))
	_, _ = tail.Write([]byte("3t-pw\n" + strings.Repeat("x", 495) + "s3cr3t-pw\n"))
	if strings.Contains(logged.String(), "s3cr") {
		t.Fatalf("log leaked a credential: %q", logged.String())
	}
	if !strings.Contains(logged.String(), "child: auth failed for [redacted]") {
		t.Fatalf("log = %q", logged.String())
	}
}
