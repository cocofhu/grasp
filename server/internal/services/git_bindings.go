package services

import "github.com/cocofhu/grasp/internal/runtime"

func withoutLegacyGitBindings(bindings map[string]*string) map[string]*string {
	if bindings == nil {
		return nil
	}
	out := make(map[string]*string, len(bindings))
	for key, value := range bindings {
		if !runtime.IsGitAuthorizationEnvKey(key) {
			out[key] = value
		}
	}
	return out
}

func withoutLegacyGitEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		if !runtime.IsGitAuthorizationEnvKey(key) {
			out[key] = value
		}
	}
	return out
}
