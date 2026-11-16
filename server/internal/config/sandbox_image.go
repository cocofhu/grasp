package config

import (
	"os"
	"sort"
	"strings"
)

const retiredSandboxImageEnv = "GRASP_SANDBOX_IMAGE"

// retiredSandboxImageKeys names the retired image settings that are still
// present, so operators can remove them. Values are never logged.
func retiredSandboxImageKeys(c *Config) []string {
	var keys []string
	if strings.TrimSpace(c.Sandbox.RetiredImage) != "" {
		keys = append(keys, "sandbox.image")
	}
	for backend, image := range c.Sandbox.RetiredImages {
		if strings.TrimSpace(image) != "" {
			keys = append(keys, "sandbox.images."+backend)
		}
	}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.TrimSpace(value) == "" {
			continue
		}
		if name == retiredSandboxImageEnv || strings.HasPrefix(name, retiredSandboxImageEnv+"_") {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}
