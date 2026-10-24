package runtime

import "testing"

func TestGenericCredentialBindingsCannotChooseModelOrGit(t *testing.T) {
	id := "credential"
	env := map[string]string{"CUSTOM_TOKEN": "old"}
	bindings := map[string]*string{"CUSTOM_TOKEN": &id, "GRASP_CURSOR_API_KEY": &id, "GITHUB_TOKEN": &id, "GRASP_ARTIFACT_TOKEN": &id}
	ApplyCredentialBindings(env, nil, bindings, func(string) (string, string, bool) { return "CUSTOM_TOKEN", "selected", true })
	if env["CUSTOM_TOKEN"] != "selected" {
		t.Fatal("non-model explicit credential was not applied")
	}
	for _, key := range []string{"GRASP_CURSOR_API_KEY", "GITHUB_TOKEN", "GRASP_ARTIFACT_TOKEN"} {
		if env[key] != "" {
			t.Fatalf("generic credential binding chose protected source %s", key)
		}
	}
	ApplyCredentialBindings(env, nil, map[string]*string{"CUSTOM_TOKEN": nil}, nil)
	if env["CUSTOM_TOKEN"] != "" {
		t.Fatal("explicitly disabled generic credential survived")
	}
}
