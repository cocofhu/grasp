package services

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
)

const (
	EnvGitSSHPrivateKey = envauth.EnvGitSSHPrivateKey
	EnvGitSSHKnownHosts = envauth.EnvGitSSHKnownHosts
)

// ErrSecretEnvKey is returned when an Agent / shared Agent env carries a
// secret key (envauth.SecretEnvKeys); secrets are configured only through
// project credentials.
var ErrSecretEnvKey = errors.New("密钥请在项目凭据中配置，环境变量不支持")

// RejectSecretEnvKeys fails when env declares any envauth.SecretEnvKeys entry.
func RejectSecretEnvKeys(env map[string]string) error {
	var hits []string
	for k := range env {
		if envauth.IsSecretEnvKey(k) {
			hits = append(hits, strings.TrimSpace(k))
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sort.Strings(hits)
	return fmt.Errorf("%w %s", ErrSecretEnvKey, strings.Join(hits, " / "))
}
