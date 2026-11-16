package crypto_test

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/crypto"
)

// Ensures the production wiring path (config: security.secrets_key, not only
// GRASP_SECRETS_KEY) is enough for Encrypt/Decrypt to work.
func TestKeySourceFromConfigYAML(t *testing.T) {
	t.Setenv(crypto.SecretsKeyEnv, "") // env must NOT be required
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(k)
	config.StoreConfig(&config.Config{
		Security: config.SecurityConfig{SecretsKey: encoded},
	})
	crypto.SetKeySource(func() string {
		c := config.GetConfig()
		if c == nil {
			return ""
		}
		return c.SecretsKey()
	})
	t.Cleanup(func() {
		crypto.SetKeySource(func() string { return "" })
	})

	enc, err := crypto.Encrypt("from-yaml")
	if err != nil {
		t.Fatalf("Encrypt via config key: %v", err)
	}
	dec, err := crypto.Decrypt(enc)
	if err != nil || dec != "from-yaml" {
		t.Fatalf("Decrypt: got %q err=%v", dec, err)
	}
}
