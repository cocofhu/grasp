package agents

import (
	"os"
	"strings"

	"backend/internal/envutil"
	"backend/internal/provider"
	"backend/internal/provider/acpx"
)

const (
	envTraeCLIToken = "TRAECLI_PERSONAL_ACCESS_TOKEN"
	envTraeRegion   = "ACP_TRAE_REGION"
	envTraeCLIHost  = "TRAECLI_HOST"
	traeIntlHost    = "https://www.trae.ai"
)

// traeProvider spawns `traecli acp serve`; unlike the other ACP CLIs Trae takes
// its model on argv.
func traeProvider() provider.Provider {
	return acpx.New(acpx.Spec{
		AgentName:  provider.Trae,
		Runtime:    "trae-acp",
		ConfigRoot: "/root/.trae",
		ArgvFn:     traeArgv,
		AuthEnvFn:  traeAuthEnv,
	})
}

func traeArgv(model string) []string {
	if model == "" {
		model = "auto"
	}
	return []string{"traecli", "acp", "serve", "--model", model}
}

// traeAuthEnv normalizes the token alias and maps an international
// ACP_TRAE_REGION onto TRAECLI_HOST unless the host is set explicitly; the CN
// default leaves the host unset.
func traeAuthEnv(env []string) []string {
	if val := envutil.FirstNonEmpty("ACP_TRAE_API_KEY", "TRAE_API_KEY", envTraeCLIToken); val != "" {
		env = envutil.Upsert(env, envTraeCLIToken, val)
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envTraeRegion))) {
	case "intl", "international", "public", "ai":
		if strings.TrimSpace(os.Getenv(envTraeCLIHost)) == "" {
			env = envutil.Upsert(env, envTraeCLIHost, traeIntlHost)
		}
	}
	return env
}
