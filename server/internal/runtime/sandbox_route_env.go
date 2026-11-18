package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// readRouteEnvScript prints the bridge's route variables. Exec sessions (E2B,
// SSH) lack the bridge's environment, and re-prime rewrites only the runtime
// env file, so that file replaces the session's own route values.
const readRouteEnvScript = `f="${GRASP_RUNTIME_RUN_DIR:-/run/grasp-runtime}/env"
if [ -r "$f" ]; then unset ` + EnvACPBridgeModel + ` ` + EnvModelConfigurationID + ` ` + EnvACPBridgeReasoningEffort + ` ` + EnvACPBridgeServiceTier + `; . "$f" >/dev/null 2>&1; fi
printf '` + EnvACPBridgeModel + `=%s\n' "${` + EnvACPBridgeModel + `:-}"
printf '` + EnvModelConfigurationID + `=%s\n' "${` + EnvModelConfigurationID + `:-}"
printf '` + EnvACPBridgeReasoningEffort + `=%s\n' "${` + EnvACPBridgeReasoningEffort + `:-}"
printf '` + EnvACPBridgeServiceTier + `=%s\n' "${` + EnvACPBridgeServiceTier + `:-}"
`

// readSandboxRouteEnv recovers the model route a running sandbox was primed
// with. Unreadable values stay empty, which means "not reported".
func readSandboxRouteEnv(ctx context.Context, sb *sandbox.Sandbox) map[string]string {
	out := map[string]string{EnvACPBridgeModel: "", EnvModelConfigurationID: "", EnvACPBridgeReasoningEffort: "", EnvACPBridgeServiceTier: ""}
	raw, _ := sb.ExecScript(ctx, 10*time.Second, "bash", readRouteEnvScript)
	return parseRouteEnv(raw, out)
}

func parseRouteEnv(raw string, out map[string]string) map[string]string {
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimRight(line, "\r"), "=")
		if _, known := out[key]; ok && known {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}
