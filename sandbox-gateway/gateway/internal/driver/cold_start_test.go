package driver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestColdPayloadProofBindsEntireFrozenRequest(t *testing.T) {
	opts := coldCommand(t).opts
	if err := ValidateColdStartOptions(opts); err != nil {
		t.Fatal(err)
	}
	original := ColdStartPayloadSHA256(opts)
	if original != opts.ColdConfig.PayloadSHA256 || ColdStartPayloadSHA256(StartOptions{}) != "" {
		t.Fatal("payload proof not deterministic")
	}
	opts.ColdConfig.Env["MODEL_SECRET"] = "synthetic-altered"
	if original == ColdStartPayloadSHA256(opts) || ValidateColdStartOptions(opts) == nil {
		t.Fatal("unbound authorization change accepted")
	}
	for _, value := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		if ColdSHA256(value) {
			t.Fatal("invalid digest accepted")
		}
	}
}

func TestColdPayloadRejectsUnsafeOrIncompleteConfigurationBeforeIO(t *testing.T) {
	base := coldCommand(t).opts
	for _, name := range []string{"nil", "native", "nonce", "input", "bootstrap", "runtime-url", "runtime-header", "no-env", "env-name", "env-null", "env-size", "relative-root", "workspace-root", "git-clone", "missing-password", "runtime-mismatch", "inject-legacy", "duplicate-part", "foreign-url", "foreign-destination", "bad-sha", "part-limit", "missing-part"} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(base)
			var opts StartOptions
			_ = json.Unmarshal(raw, &opts)
			c := opts.ColdConfig
			switch name {
			case "nil":
				opts.ColdConfig = nil
			case "native":
				c.ExpectedNativeIdentity = ""
			case "nonce":
				c.RequestID = "../other"
			case "input":
				c.InputSHA256 = "bad"
			case "bootstrap":
				opts.BootstrapScript = "echo unsafe"
			case "runtime-url":
				opts.RuntimeURL = "http://user:secret@platform/runtime"
			case "runtime-header":
				opts.RuntimeHeaders = "Authorization: Bearer synthetic\noutput = secret"
			case "no-env":
				c.Env = nil
			case "env-name":
				c.Env["MODEL;INJECT"] = "invalid"
			case "env-null":
				c.Env["MODEL_SECRET"] = "synthetic\x00other"
			case "env-size":
				c.Env["MODEL_SECRET"] = strings.Repeat("s", 128<<10)
			case "relative-root":
				c.Env["CONFIG_ROOT"] = "relative/config"
			case "workspace-root":
				c.Env["CONFIG_ROOT"] = "/root/workspace/repo"
			case "git-clone":
				c.Env["GIT_REPOS"] = "repo|https://unwanted.invalid/repo"
			case "missing-password":
				delete(c.Env, "ROOT_PASSWORD")
			case "runtime-mismatch":
				c.Env["GRASP_RUNTIME_URL"] = "http://other/runtime"
			case "inject-legacy":
				c.Env["SANDBOX_INJECT"] = "unverified-input"
			case "duplicate-part":
				c.Injections[1].Destination = c.Injections[0].Destination
			case "foreign-url":
				c.Injections[0].URL = "file:///private"
			case "foreign-destination":
				c.Injections[0].Destination = "/root/workspace/repo"
			case "bad-sha":
				c.Injections[0].SHA256 = "unknown"
			case "part-limit":
				c.Injections[0].Bytes = 64<<20 + 1
			case "missing-part":
				c.Injections = c.Injections[:1]
			}
			if opts.ColdConfig != nil {
				opts.ColdConfig.PayloadSHA256 = ColdStartPayloadSHA256(opts)
			}
			if ValidateColdStartOptions(opts) == nil {
				t.Fatal("unsafe full authorization accepted")
			}
		})
	}
}
