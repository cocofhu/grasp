package services

import (
	"github.com/cocofhu/grasp/internal/sandbox"
)

// ApplyProjectSSHToSpec attaches the project credential SSH key / known_hosts
// for file inject and strips GIT_SSH_* from Spec.Env.
func ApplyProjectSSHToSpec(spec *sandbox.Spec, projectCreds map[string]string) {
	if spec == nil {
		return
	}
	sandbox.ApplySSHCredentials(spec, projectCreds[EnvGitSSHPrivateKey], projectCreds[EnvGitSSHKnownHosts])
}
