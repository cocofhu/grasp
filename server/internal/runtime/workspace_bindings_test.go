package runtime

import "testing"

func TestCredentialBindingsDoNotFallBackAndPreserveUnboundDefaults(t *testing.T) {
	good := "chosen"
	bad := "revoked"
	wrong := "other-kind"
	env := map[string]string{"CUSTOM": "default", "DISABLED": "default", "REVOKED": "default", "WRONG": "default", "INHERITED": "keep", "GRASP_RUN_ID": "workspace"}
	creds := map[string]string{"CUSTOM": "default", "DISABLED": "default", "REVOKED": "default", "WRONG": "default"}
	ApplyCredentialBindings(env, creds, map[string]*string{"CUSTOM": &good, "DISABLED": nil, "REVOKED": &bad, "WRONG": &wrong, "GRASP_RUN_ID": &good}, func(id string) (string, string, bool) {
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

func TestModelWriteBackIdentityRequiresResolvedConfiguration(t *testing.T) {
	if ModelCredentialIDForWriteBack(nil) != "" {
		t.Fatal("unresolved model acquired credential identity")
	}
	if ModelCredentialIDForWriteBack(map[string]string{EnvModelCredentialID: "selected"}) != "selected" {
		t.Fatal("resolved model identity lost")
	}
}
