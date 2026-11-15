package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runRouteEnvScript(t *testing.T, env ...string) map[string]string {
	t.Helper()
	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(readRouteEnvScript)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	return parseRouteEnv(string(out), map[string]string{EnvACPBridgeModel: "", EnvModelConfigurationID: "", EnvACPBridgeReasoningEffort: ""})
}

func TestReadRouteEnvPrefersRuntimeEnvFile(t *testing.T) {
	dir := t.TempDir()
	file := "declare -x ACP_BRIDGE_MODEL=\"claude-opus-5-5-medium\"\n" +
		"declare -x GRASP_MODEL_CONFIGURATION_ID=\"model-d620734c\"\n" +
		"declare -rx UID=\"0\"\n"
	if err := os.WriteFile(filepath.Join(dir, "env"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	// An exec session without the bridge environment (E2B), or with the
	// pre-re-prime container environment (Docker), must not hide the route.
	got := runRouteEnvScript(t, "GRASP_RUNTIME_RUN_DIR="+dir, "ACP_BRIDGE_MODEL=stale-model", "ACP_BRIDGE_REASONING_EFFORT=high")
	if got[EnvACPBridgeModel] != "claude-opus-5-5-medium" || got[EnvModelConfigurationID] != "model-d620734c" {
		t.Fatalf("route env = %#v", got)
	}
	if got[EnvACPBridgeReasoningEffort] != "" {
		t.Fatalf("a value cleared by re-prime must not fall back to the session: %#v", got)
	}
}

func TestReadRouteEnvWithoutRuntimeEnvFile(t *testing.T) {
	got := runRouteEnvScript(t, "GRASP_RUNTIME_RUN_DIR="+t.TempDir(), "ACP_BRIDGE_MODEL=env-model")
	if got[EnvACPBridgeModel] != "env-model" || got[EnvModelConfigurationID] != "" || got[EnvACPBridgeReasoningEffort] != "" {
		t.Fatalf("route env = %#v", got)
	}
}

func TestParseRouteEnvIgnoresUnknownAndKeepsEmpty(t *testing.T) {
	got := parseRouteEnv("noise\nOTHER=x\nACP_BRIDGE_MODEL=a=b\r\nGRASP_MODEL_CONFIGURATION_ID=\n", map[string]string{EnvACPBridgeModel: "", EnvModelConfigurationID: "", EnvACPBridgeReasoningEffort: ""})
	if len(got) != 3 || got[EnvACPBridgeModel] != "a=b" || got[EnvModelConfigurationID] != "" {
		t.Fatalf("parsed = %#v", got)
	}
}
