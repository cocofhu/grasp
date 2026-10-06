package services

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// ValidateRunSandboxEnv validates optional StartRun env entries.
// Rules:
//   - rows with both Key (trimmed empty) and Value empty are ignored
//   - Key empty with non-empty Value → reject (lists row index)
//   - duplicate keys → reject
//   - denied reserved/auth keys → reject
//   - every kept row is effective; empty Value is kept (override-to-empty)
//
// On success returns a clean snapshot slice (nil when nothing to apply).
// On failure the error lists problem keys / row hints; callers must not create a Run.
func ValidateRunSandboxEnv(in []models.EnvEntry) ([]models.EnvEntry, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]models.EnvEntry, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	var problems []string
	for i, e := range in {
		k := strings.TrimSpace(e.Key)
		if k == "" && e.Value == "" {
			continue
		}
		if k == "" {
			problems = append(problems, fmt.Sprintf("row %d (missing key)", i+1))
			continue
		}
		if _, ok := seen[k]; ok {
			problems = append(problems, k+" (duplicate)")
			continue
		}
		seen[k] = struct{}{}
		if runtime.IsDeniedRunSandboxEnvKey(k) {
			problems = append(problems, k)
			continue
		}
		out = append(out, models.EnvEntry{Key: k, Value: e.Value, Secret: e.Secret})
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid run sandbox env: %s", strings.Join(problems, ", "))
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
