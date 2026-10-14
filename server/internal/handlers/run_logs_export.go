package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

// ExportRunLogs streams the full logs of a run as a single plain-text file for
// offline error diagnosis. It aggregates persisted state (not the live-only
// in-flight buffer), so it is authoritative once nodes have been saved:
//   - run metadata + FSM trace
//   - every node execution in iteration order: status, error, agent events,
//     MCP calls, output markdown
//   - archived sandbox container (docker) logs, emitted once per container
//     alongside the node's first execution
func (h *Handlers) ExportRunLogs(c *gin.Context) {
	runID := c.Param("id")
	run, ok := h.Runs.Get(runID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	var b strings.Builder
	writeRunHeader(&b, run)
	writeTrace(&b, run.Trace)

	states := h.Runs.States(runID)
	sbxByNode := map[string][]models.SandboxLog{}
	for _, l := range h.Sbx.RunSandboxLogs(runID) {
		sbxByNode[l.NodeID] = append(sbxByNode[l.NodeID], l)
	}
	usedSbx := map[string]bool{}
	for _, s := range states {
		writeStateRun(&b, s, sbxByNode[s.NodeID], usedSbx)
	}
	// Emit any sandbox logs whose node had no state_run rows (e.g. setup-only
	// containers) so nothing is silently dropped from the export.
	for nodeID, logs := range sbxByNode {
		for _, l := range logs {
			if usedSbx[l.Name] {
				continue
			}
			b.WriteString(fmt.Sprintf("\n=== Sandbox (docker) Log  node=%s  container=%s ===\n", nodeID, l.Name))
			b.WriteString(strings.TrimRight(l.Content, "\n"))
			b.WriteString("\n")
		}
	}

	filename := fmt.Sprintf("%s-logs.txt", sanitizeFilename(runID))
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(b.String()))
}

// sanitizeFilename keeps only characters safe for a Content-Disposition
// filename (alnum, dash, underscore, dot), replacing anything else with '_'.
// runID is server-generated so this is defense-in-depth against header
// injection / quote-breaking rather than an expected code path.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "run"
	}
	return out
}

func writeRunHeader(b *strings.Builder, run models.Run) {
	b.WriteString("=== Grasp Run Log Export ===\n")
	b.WriteString(fmt.Sprintf("Run: %s\n", run.ID))
	wf := run.WorkflowName
	if run.WorkflowVersion > 0 {
		wf = fmt.Sprintf("%s (v%d)", wf, run.WorkflowVersion)
	}
	b.WriteString(fmt.Sprintf("Workflow: %s\n", wf))
	b.WriteString(fmt.Sprintf("Status: %s\n", run.Status))
	b.WriteString(fmt.Sprintf("Trigger: %s\n", run.Trigger))
	b.WriteString(fmt.Sprintf("Started: %s\n", run.StartedAt.Format(time.RFC3339)))
	if run.Branch != "" {
		b.WriteString(fmt.Sprintf("Branch: %s\n", run.Branch))
	}
	b.WriteString(fmt.Sprintf("Duration: %ds\n", run.DurationSec))
	b.WriteString(fmt.Sprintf("Exported: %s\n", time.Now().Format(time.RFC3339)))
}

func writeTrace(b *strings.Builder, trace []models.TraceEntry) {
	if len(trace) == 0 {
		return
	}
	b.WriteString("\n=== FSM Trace ===\n")
	for _, e := range trace {
		line := fmt.Sprintf("[%s] %s node=%s", e.At, e.Event, e.NodeID)
		if e.Iteration > 0 {
			line += fmt.Sprintf(" iter=%d", e.Iteration)
		}
		if e.To != "" {
			line += " -> " + e.To
		}
		if e.Detail != "" {
			line += "  " + e.Detail
		}
		b.WriteString(line + "\n")
	}
}

func writeStateRun(b *strings.Builder, s models.StateRun, sbxLogs []models.SandboxLog, usedSbx map[string]bool) {
	b.WriteString(fmt.Sprintf(
		"\n=== Node %s (%s)  iteration %d  status=%s duration=%ds ===\n",
		s.NodeID, s.NodeType, s.Iteration, s.Status, s.DurationSec,
	))

	if strings.TrimSpace(s.Error) != "" {
		b.WriteString("-- Error --\n")
		b.WriteString(strings.TrimRight(s.Error, "\n") + "\n")
	}

	if len(s.Events) > 0 {
		b.WriteString("-- Agent Events --\n")
		for _, ev := range s.Events {
			b.WriteString(formatEvent(ev))
		}
	}

	if len(s.McpCalls) > 0 {
		b.WriteString("-- MCP Calls --\n")
		for _, m := range s.McpCalls {
			line := fmt.Sprintf("[%s] %s", m.At, m.Tool)
			if m.IsError {
				line += " (error)"
			}
			b.WriteString(line + "\n")
			if m.Args != "" {
				b.WriteString("  args: " + m.Args + "\n")
			}
			if m.Result != "" {
				b.WriteString("  result: " + m.Result + "\n")
			}
		}
	}

	if strings.TrimSpace(s.OutputMd) != "" {
		b.WriteString("-- Output (md) --\n")
		b.WriteString(strings.TrimRight(s.OutputMd, "\n") + "\n")
	}

	if h, _ := s.Outputs[models.OutputHandoff].(string); strings.TrimSpace(h) != "" {
		src, _ := s.Outputs[models.OutputHandoffSource].(string)
		if src == "" {
			src = models.HandoffSourceAgent
		}
		b.WriteString("-- Handoff (" + src + ") --\n")
		b.WriteString(strings.TrimRight(h, "\n") + "\n")
	}

	// Sandbox logs are keyed per container (node), not per iteration; emit them
	// once (on the first state_run row for the node) to avoid duplication.
	for _, l := range sbxLogs {
		if usedSbx[l.Name] {
			continue
		}
		usedSbx[l.Name] = true
		b.WriteString(fmt.Sprintf("-- Sandbox (docker) Log  container=%s --\n", l.Name))
		b.WriteString(strings.TrimRight(l.Content, "\n") + "\n")
	}
}

func formatEvent(ev models.AcpEvent) string {
	head := fmt.Sprintf("[+%ds] %s", ev.T, ev.Kind)
	if ev.Title != "" {
		head += " " + ev.Title
	}
	if ev.Status != "" {
		head += " (" + ev.Status + ")"
	}
	out := head + "\n"
	if ev.Text != "" {
		for _, ln := range strings.Split(strings.TrimRight(ev.Text, "\n"), "\n") {
			out += "    " + ln + "\n"
		}
	}
	if ev.Artifact != nil {
		out += fmt.Sprintf("    [artifact] %s (%s)\n", ev.Artifact.Name, ev.Artifact.Kind)
	}
	return out
}
