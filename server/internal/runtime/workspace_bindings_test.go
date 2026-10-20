package runtime

import "testing"

func TestCredentialBindingsDoNotFallBackAndPreserveUnboundDefaults(t *testing.T) {
	good := "chosen"
	bad := "revoked"
	wrong := "other-kind"
	env := map[string]string{"CUSTOM": "default", "DISABLED": "default", "REVOKED": "default", "WRONG": "default", "INHERITED": "keep", "GRASP_RUN_ID": "workspace"}
	creds := map[string]string{"CUSTOM": "default", "DISABLED": "default", "REVOKED": "default", "WRONG": "default"}
	ApplyCredentialBindings(env, creds, "workspace", map[string]*string{"CUSTOM": &good, "DISABLED": nil, "REVOKED": &bad, "WRONG": &wrong, "GRASP_RUN_ID": &good}, func(pid, id string) (string, string, bool) {
		if pid != "workspace" {
			t.Fatal("wrong workspace")
		}
		switch id {
		case good:
			return "CUSTOM", "selected", true
		case wrong:
			return "CUSTOM", "other", true
		}
		return "", "", false
	})
	if env["CUSTOM"] != "selected" || creds["CUSTOM"] != "selected" || env["INHERITED"] != "keep" || env["GRASP_RUN_ID"] != "workspace" {
		t.Fatalf("bindings = %v %v", env, creds)
	}
	for _, k := range []string{"DISABLED", "REVOKED", "WRONG"} {
		if _, ok := env[k]; ok {
			t.Fatalf("%s fell back", k)
		}
		if _, ok := creds[k]; ok {
			t.Fatalf("%s credentials fell back", k)
		}
	}
}

func TestCredentialWriteBackIdentityFollowsSelectionWithoutFallback(t *testing.T) {
	resolve := func(projectID, id string) (string, string, bool) {
		return "CODEX_LOGIN", "injected", projectID == "workspace" && id == "selected"
	}
	inherited := func(_, _, _ string) string { return "default" }
	selected, stale := "selected", "revoked"
	for _, tc := range []struct {
		name     string
		choice   string
		bindings map[string]*string
		want     string
	}{
		{"inherited", "", nil, "default"},
		{"chosen", "selected", nil, "selected"},
		{"revoked", "revoked", nil, ""},
		{"bound", "", map[string]*string{"CODEX_LOGIN": &selected}, "selected"},
		{"disabled", "selected", map[string]*string{"CODEX_LOGIN": nil}, ""},
		{"revoked binding", "selected", map[string]*string{"CODEX_LOGIN": &stale}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CredentialIDForWriteBack("workspace", "CODEX_LOGIN", "injected", tc.choice, tc.bindings, resolve, inherited)
			if got != tc.want {
				t.Fatalf("identity=%q want %q", got, tc.want)
			}
		})
	}
}
