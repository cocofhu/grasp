package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// ResolveNotifyEvents returns the effective event set for a workflow under a
// project policy. Aligns with Demo page.html resolveEvents:
//
//	enabled=false → empty (hard close)
//	mode=off      → empty
//	mode=custom   → workflow events (may be empty)
//	inherit/else  → project defaultEvents
//
// waiting_human, failed, and completed may be delivered when present in the
// resolved set; unknown kinds are stripped. DefaultEvents still default to
// waiting_human+failed (completed is opt-in).
func ResolveNotifyEvents(project models.ProjectNotifyPolicy, workflow models.WorkflowNotifyPolicy) []string {
	if !project.IsEnabled() {
		return nil
	}
	var raw []string
	switch workflow.EffectiveMode() {
	case models.NotifyModeOff:
		return nil
	case models.NotifyModeCustom:
		raw = append([]string(nil), workflow.Events...)
	default: // inherit
		raw = project.EffectiveDefaultEvents()
	}
	return filterP0NotifyEvents(raw)
}

// NotifyEventAllowed reports whether kind is in the resolved event set.
func NotifyEventAllowed(events []string, kind string) bool {
	kind = strings.TrimSpace(kind)
	for _, e := range events {
		if e == kind {
			return true
		}
	}
	return false
}

func filterP0NotifyEvents(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 3)
	for _, e := range in {
		switch strings.TrimSpace(e) {
		case models.NotifyKindWaitingHuman, models.NotifyKindFailed, models.NotifyKindCompleted:
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// NormalizeProjectNotifyPolicy sanitizes a project policy for persistence.
// Nil Enabled defaults to true; nil DefaultEvents defaults to waiting_human+failed
// (completed is not added automatically). Known kinds including completed may be
// stored; unknown kinds are stripped. Template fields: whitespace-only is trimmed
// to ""; empty is NOT rewritten to the default QQ body.
// ChannelIDs is the explicit target list, de-duped & trimmed; nil is stored as
// empty ("deliver to none").
func NormalizeProjectNotifyPolicy(p models.ProjectNotifyPolicy) models.ProjectNotifyPolicy {
	if p.Enabled == nil {
		on := true
		p.Enabled = &on
	}
	if p.DefaultEvents == nil {
		p.DefaultEvents = []string{models.NotifyKindWaitingHuman, models.NotifyKindFailed}
	} else {
		p.DefaultEvents = normalizeStoredEvents(p.DefaultEvents)
	}
	p.ChannelIDs = NormalizeNotifyChannelIDs(p.ChannelIDs)
	if strings.TrimSpace(p.WaitingHumanTemplate) == "" {
		p.WaitingHumanTemplate = ""
	}
	if strings.TrimSpace(p.FailedTemplate) == "" {
		p.FailedTemplate = ""
	}
	if strings.TrimSpace(p.CompletedTemplate) == "" {
		p.CompletedTemplate = ""
	}
	return p
}

// NormalizeNotifyChannelIDs trims, drops empties, and de-duplicates channel ids.
func NormalizeNotifyChannelIDs(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// NormalizeWorkflowNotifyPolicy sanitizes a workflow override for persistence.
func NormalizeWorkflowNotifyPolicy(w models.WorkflowNotifyPolicy) models.WorkflowNotifyPolicy {
	switch w.Mode {
	case models.NotifyModeOff, models.NotifyModeInherit, models.NotifyModeCustom:
		// ok
	case "":
		w.Mode = models.NotifyModeInherit
	default:
		w.Mode = models.NotifyModeInherit
	}
	if w.Mode != models.NotifyModeCustom {
		// Keep events for round-trip when switching back to custom, but still
		// normalize kinds so junk does not accumulate.
		w.Events = normalizeStoredEvents(w.Events)
	} else {
		w.Events = normalizeStoredEvents(w.Events)
	}
	return w
}

func normalizeStoredEvents(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.TrimSpace(e)
		switch e {
		case models.NotifyKindWaitingHuman, models.NotifyKindFailed, models.NotifyKindCompleted:
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// WorkflowNotifyPoliciesEqual compares workflow overrides.
func WorkflowNotifyPoliciesEqual(a, b models.WorkflowNotifyPolicy) bool {
	a = NormalizeWorkflowNotifyPolicy(a)
	b = NormalizeWorkflowNotifyPolicy(b)
	if a.Mode != b.Mode {
		return false
	}
	return stringSlicesEqual(a.Events, b.Events)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
