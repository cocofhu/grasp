package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformRuleServiceSeedAndPriority(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "global")
	profiles := filepath.Join(root, "profiles")
	svc, err := NewPlatformRuleService(global, profiles)
	if err != nil {
		t.Fatal(err)
	}

	files, err := svc.ruleFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 11 {
		t.Fatalf("rule files = %d, want 11", len(files))
	}
	foundApprove, foundPreflight := false, false
	for _, f := range files {
		if f == "approve.md" {
			foundApprove = true
		}
		if f == "preflight.md" {
			foundPreflight = true
		}
	}
	if !foundApprove {
		t.Fatal("expected approve.md in platform rule files")
	}
	if !foundPreflight {
		t.Fatal("expected preflight.md in platform rule files")
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(global, f)); err != nil {
			t.Fatalf("seed missing %s: %v", f, err)
		}
	}

	custom := "# custom global test"
	if _, err := svc.SaveGlobal("test.md", custom); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetGlobal("test.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != custom || got.Source != RuleSourceGlobal {
		t.Fatalf("global save: %+v", got)
	}

	agent := "DemoAgent"
	override := "# agent override"
	if _, err := svc.SaveAgent(agent, "test.md", override); err != nil {
		t.Fatal(err)
	}
	agentGot, err := svc.GetAgent(agent, "test.md")
	if err != nil {
		t.Fatal(err)
	}
	if agentGot.Content != override || agentGot.Source != RuleSourceOverride {
		t.Fatalf("agent override: %+v", agentGot)
	}

	if err := svc.DeleteAgent(agent, "test.md"); err != nil {
		t.Fatal(err)
	}
	agentGot, err = svc.GetAgent(agent, "test.md")
	if err != nil {
		t.Fatal(err)
	}
	if agentGot.Content != custom || agentGot.Source != RuleSourceGlobal {
		t.Fatalf("after delete override: %+v", agentGot)
	}

	if err := svc.DeleteGlobal("test.md"); err != nil {
		t.Fatal(err)
	}
	embed, err := svc.ReadEmbedDefault("test.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err = svc.GetGlobal("test.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != RuleSourceEmbed || got.Content != embed.Content {
		t.Fatalf("embed fallback: %+v", got)
	}

	if err := svc.validateFile("../base.md"); err == nil {
		t.Fatal("path traversal should be rejected")
	}

	listed, err := svc.ListGlobal()
	if err != nil || len(listed) == 0 {
		t.Fatalf("ListGlobal: %v %d", err, len(listed))
	}
	reset, err := svc.ResetGlobal("test.md")
	if err != nil || reset.Content == "" {
		t.Fatalf("ResetGlobal: %+v err=%v", reset, err)
	}
	if err := svc.validateFile(""); err == nil {
		t.Fatal("empty file")
	}
	if err := svc.validateFile("nope.md"); err == nil {
		t.Fatal("unknown file")
	}
	if _, err := svc.GetGlobal(""); err == nil {
		t.Fatal("empty get")
	}
	if _, err := svc.SaveGlobal("nope.md", "x"); err == nil {
		t.Fatal("unknown save")
	}
	if err := svc.DeleteGlobal("nope.md"); err == nil {
		t.Fatal("unknown delete")
	}
	if _, err := svc.ResetGlobal("nope.md"); err == nil {
		t.Fatal("unknown reset")
	}
	if _, err := svc.ReadEmbedDefault("nope.md"); err == nil {
		t.Fatal("unknown embed")
	}
	// Seed is idempotent when files exist
	if err := svc.Seed(); err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListAgent("DemoAgent")
	if err != nil || len(items) == 0 {
		t.Fatalf("ListAgent: %v", err)
	}
}
