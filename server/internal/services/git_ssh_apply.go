package services

import (
	"github.com/cocofhu/grasp/internal/sandbox"
)

// ApplySSHCredentialsToSpec attaches the workspace credential SSH key /
// known_hosts for file inject and strips GIT_SSH_* from Spec.Env.
func ApplySSHCredentialsToSpec(spec *sandbox.Spec, creds map[string]string) {
	if spec == nil {
		return
	}
	sandbox.ApplySSHCredentials(spec, creds[EnvGitSSHPrivateKey], creds[EnvGitSSHKnownHosts])
}
