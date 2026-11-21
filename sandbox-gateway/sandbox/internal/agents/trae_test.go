package agents

import (
	"slices"
	"testing"

	"backend/internal/envutil"
)

func TestTraeArgv(t *testing.T) {
	if got := traeArgv(""); !slices.Equal(got, []string{"traecli", "acp", "serve", "--model", "auto"}) {
		t.Fatalf("default model argv = %q", got)
	}
	if got := traeArgv("doubao"); got[len(got)-1] != "doubao" {
		t.Fatalf("model not on argv: %q", got)
	}
}

func TestTraeAuthEnv(t *testing.T) {
	t.Setenv("TRAECLI_PERSONAL_ACCESS_TOKEN", "")
	t.Setenv("TRAE_API_KEY", "")
	t.Setenv("ACP_TRAE_API_KEY", "tok")
	t.Setenv("TRAECLI_HOST", "")

	t.Setenv("ACP_TRAE_REGION", "intl")
	env := traeAuthEnv(nil)
	if envutil.Get(env, "TRAECLI_PERSONAL_ACCESS_TOKEN") != "tok" {
		t.Fatalf("token alias not normalized: %q", env)
	}
	if envutil.Get(env, "TRAECLI_HOST") != traeIntlHost {
		t.Fatalf("intl region must set the host: %q", env)
	}

	t.Setenv("ACP_TRAE_REGION", "cn")
	if host := envutil.Get(traeAuthEnv(nil), "TRAECLI_HOST"); host != "" {
		t.Fatalf("cn region must leave the host unset, got %q", host)
	}

	t.Setenv("ACP_TRAE_REGION", "intl")
	t.Setenv("TRAECLI_HOST", "https://custom.example")
	if slices.Contains(traeAuthEnv(nil), "TRAECLI_HOST="+traeIntlHost) {
		t.Fatal("an explicit host must win over the region default")
	}
}
