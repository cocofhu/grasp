package mcp

import (
	"sort"
	"strings"
	"testing"
)

func toolNames(defs []map[string]any) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		name, _ := d["name"].(string)
		out = append(out, name)
	}
	return out
}

// TestBuiltinToolsDeclaredAndHandled keeps tool declarations and dispatch in
// step: every built-in tool tools/list can carry has exactly one handler, and
// tools dispatched by their own files are never also in the registry.
func TestBuiltinToolsDeclaredAndHandled(t *testing.T) {
	declared := map[string]bool{}
	for _, name := range append(toolNames(artifactTools()), toolNames([]map[string]any{conversationHistoryTool(), referenceTool()})...) {
		if declared[name] {
			t.Errorf("tool %q declared twice", name)
		}
		declared[name] = true
		if _, ok := builtinToolHandlers[name]; !ok {
			t.Errorf("tool %q is declared but has no handler", name)
		}
	}
	// Called only by the sandbox artifact-upload CLI, so never listed to agents.
	unlisted := map[string]bool{"upload_image_artifact": true}
	var extra []string
	for name := range builtinToolHandlers {
		if !declared[name] && !unlisted[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("handlers without a declaration: %s", strings.Join(extra, ", "))
	}
	for _, name := range append(append(toolNames(fileTools()), toolNames(pageTools())...), toolNames(liveTools())...) {
		if _, ok := builtinToolHandlers[name]; ok {
			t.Errorf("tool %q has its own dispatcher and must not be registered", name)
		}
		if declared[name] {
			t.Errorf("tool %q is declared twice across tool files", name)
		}
	}
}

// TestRunToolUnknownName keeps the unknown-tool error wording.
func TestRunToolUnknownName(t *testing.T) {
	h := NewHost(nil)
	if msg, isErr := h.runTool("run-x", "tok", "no_such_tool", nil); !isErr || msg != "unknown tool: no_such_tool" {
		t.Fatalf("got %q isErr=%v", msg, isErr)
	}
}
