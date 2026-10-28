package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
)

func TestKimiCodeAuthAndSite(t *testing.T) {
	if DefaultConfigRoot(BackendKimiCode) != "/root/.kimi-code" {
		t.Fatal(DefaultConfigRoot(BackendKimiCode))
	}
	if AuthConfigFileExists(t.TempDir(), BackendKimiCode) {
		t.Fatal("settings.json must not satisfy Kimi Code auth")
	}
	_, err := MergeAuthEnv(BackendKimiCode, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "订阅 API Key") || !strings.Contains(err.Error(), envauth.EnvKimiCodeAPIKey) {
		t.Fatalf("missing key: %v", err)
	}
	out, err := MergeAuthEnv(BackendKimiCode, map[string]string{envauth.EnvKimiCodeAPIKey: "sk-test-value"})
	if err != nil {
		t.Fatal(err)
	}
	if out["KIMI_CODE_API_KEY"] != "sk-test-value" || out[EnvKimiCodeRegion] != "intl" {
		t.Fatalf("default site: %#v", out)
	}
	out, err = MergeAuthEnv(BackendKimiCode, map[string]string{envauth.EnvKimiCodeAPIKey: "sk-test-value", EnvKimiCodeRegion: "domestic"})
	if err != nil || out[EnvKimiCodeRegion] != "cn" {
		t.Fatalf("cn site: %v %#v", err, out)
	}
	if _, err := MergeAuthEnv(BackendKimiCode, map[string]string{envauth.EnvKimiCodeAPIKey: "sk-test-value", EnvKimiCodeRegion: "eu"}); err == nil || !strings.Contains(err.Error(), "未知的 Kimi Code 服务站点") {
		t.Fatalf("unknown site: %v", err)
	}
	cn, err := KimiPlatformBaseURL("cn")
	if err != nil || cn != "https://api.kimi.com/coding/v1" {
		t.Fatal(cn, err)
	}
	intl, err := KimiPlatformBaseURL("")
	if err != nil || intl != "https://api.kimi.ai/coding/v1" {
		t.Fatal(intl, err)
	}
}

func TestKimiCodeEmptyModelStaysEmpty(t *testing.T) {
	env := map[string]string{envauth.EnvCursorAPIKey: "old-cursor-key", "KIMI_CODE_API_KEY": "old-cli-key"}
	ref := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "kimi"}
	err := ApplyModelBinding(env, "kimi_code", ref, func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{
			Backend: "kimi_code", CredentialID: "selected", Secret: "selected-test-key",
			Binding: modelrouting.Binding{Kind: "platform", CredentialID: "selected"},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if env[envauth.EnvKimiCodeAPIKey] != "selected-test-key" || env[EnvACPBridgeModel] != "" || env[envauth.EnvCursorAPIKey] != "" || env["KIMI_CODE_API_KEY"] != "" {
		t.Fatalf("empty model was replaced or the key leaked into the CLI slot: %#v", env)
	}
}
