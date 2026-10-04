package services

import (
	"testing"
)

func TestScanAndStripGroupSensitiveKeys(t *testing.T) {
	skill, orgSvc := setupFolderOrg(t)
	// setupFolderOrg uses non-token "TOKEN"; overwrite with real token keys.
	for name, env := range map[string]map[string]string{
		"alice": {
			"GRASP_CURSOR_API_KEY": "a-key",
			"GITLAB_TOKEN":         "a-gl",
			"FEATURE_FLAG":         "1",
		},
		"bob": {
			"GRASP_CURSOR_API_KEY": "b-key",
			"LOG_LEVEL":            "info",
		},
		"carol": {
			"GITLAB_TOKEN": "c-gl",
		},
		"outside": {
			"GRASP_CURSOR_API_KEY": "out-key",
			"GITLAB_TOKEN":         "out-gl",
		},
	} {
		a, ok := skill.Get(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		a.Env = env
		if err := skill.Save(a); err != nil {
			t.Fatal(err)
		}
	}

	hits, err := orgSvc.ScanGroupSensitiveKeys("g_root")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]int{}
	for _, h := range hits {
		byKey[h.Key] = h.AgentCount
	}
	if byKey["GRASP_CURSOR_API_KEY"] != 2 || byKey["GITLAB_TOKEN"] != 2 {
		t.Fatalf("hits=%+v", hits)
	}
	if _, ok := byKey["FEATURE_FLAG"]; ok {
		t.Fatal("non-token must not appear")
	}

	res, err := orgSvc.StripGroupSensitiveKeys("g_root", []string{"GITLAB_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cleared != 3 || len(res.Failed) != 0 {
		t.Fatalf("strip result=%+v", res)
	}
	alice, _ := skill.Get("alice")
	if _, ok := alice.Env["GITLAB_TOKEN"]; ok {
		t.Fatalf("alice GITLAB_TOKEN should be gone: %#v", alice.Env)
	}
	if alice.Env["GRASP_CURSOR_API_KEY"] != "a-key" || alice.Env["FEATURE_FLAG"] != "1" {
		t.Fatalf("alice should keep unselected / non-token: %#v", alice.Env)
	}
	outside, _ := skill.Get("outside")
	if outside.Env["GITLAB_TOKEN"] != "out-gl" {
		t.Fatalf("outside subtree must not be touched: %#v", outside.Env)
	}

	if _, err := orgSvc.StripGroupSensitiveKeys("g_root", []string{"FEATURE_FLAG"}); err == nil {
		t.Fatal("non-token key must be rejected")
	}
	if _, err := orgSvc.StripGroupSensitiveKeys("g_root", nil); err == nil {
		t.Fatal("empty keys must be rejected")
	}
}
