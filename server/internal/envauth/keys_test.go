package envauth

import "testing"

func TestIsPlatformAuthEnvKey(t *testing.T) {
	for _, k := range []string{
		"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "CODEBUDDY_API_KEY",
		"TRAECLI_PERSONAL_ACCESS_TOKEN", "OPENCODE_API_KEY",
	} {
		if !IsPlatformAuthEnvKey(k) {
			t.Fatalf("%s should be platform auth key", k)
		}
	}
	for _, k := range []string{
		"TRAE_API_KEY", "GITLAB_TOKEN", "GRASP_CURSOR_API_KEY", "GRASP_TRAE_API_KEY",
		"GRASP_CODEBUDDY_REGION", "GRASP_TRAE_REGION",
	} {
		if IsPlatformAuthEnvKey(k) {
			t.Fatalf("%s must not be filtered as platform auth", k)
		}
	}
}

func TestIsSecretEnvKey(t *testing.T) {
	for _, k := range SecretEnvKeys() {
		if !IsSecretEnvKey(k) {
			t.Fatalf("%s should be secret env key", k)
		}
	}
	for _, k := range []string{"GIT_SSH_KNOWN_HOSTS", "GITHUB_TOKEN", "GRASP_TRAE_API_KEY", " CURSOR_API_KEY "} {
		if !IsSecretEnvKey(k) {
			t.Fatalf("%s should be secret env key", k)
		}
	}
	for _, k := range []string{
		"TRAE_API_KEY", "GIT_REPOS", "GITHUB_URL", "GITLAB_URL",
		"GRASP_CODEBUDDY_REGION", "GRASP_TRAE_REGION", "FEATURE_FLAG",
	} {
		if IsSecretEnvKey(k) {
			t.Fatalf("%s must not be secret env key", k)
		}
	}
}

func TestOverlayEnv(t *testing.T) {
	got := OverlayEnv(
		map[string]string{"FEATURE": "s", "SHARED": "1", " ": "x"},
		map[string]string{"FEATURE": "a"},
	)
	if got["FEATURE"] != "a" || got["SHARED"] != "1" || len(got) != 2 {
		t.Fatalf("agent wins: %#v", got)
	}
}

func TestStripSecretEnvKeys(t *testing.T) {
	got := StripSecretEnvKeys(map[string]string{"GITHUB_TOKEN": "t", "CURSOR_API_KEY": "c", "FEATURE": "1"})
	if len(got) != 1 || got["FEATURE"] != "1" {
		t.Fatalf("got %#v", got)
	}
}
