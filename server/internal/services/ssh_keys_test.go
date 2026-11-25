package services

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"golang.org/x/crypto/ssh"
)

func ed25519Line(t *testing.T, comment string) (ssh.PublicKey, string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	if comment != "" {
		line += " " + comment
	}
	return key, line
}

func rsaLine(t *testing.T, bits int) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(ssh.MarshalAuthorizedKey(key))
}

func newSSHKeyFixture(t *testing.T) (*SSHKeyService, *config.Config) {
	t.Helper()
	cfg := &config.Config{Auth: config.AuthConfig{Users: []config.AuthUser{
		{Username: "alice", PasswordHash: "h"},
		{Username: "root-admin", PasswordHash: "h", IsAdmin: true},
	}}}
	return NewSSHKeyService(newTestDB(t), func() *config.Config { return cfg }), cfg
}

func TestSSHKeyAddListDelete(t *testing.T) {
	s, _ := newSSHKeyFixture(t)
	key, line := ed25519Line(t, "alice@laptop")
	row, err := s.Add("alice", "", "  "+line+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "alice@laptop" || row.Fingerprint != ssh.FingerprintSHA256(key) || strings.Contains(row.PublicKey, "laptop") {
		t.Fatalf("row = %+v", row)
	}
	if _, err := s.Add("bob", "dup", line); !errors.Is(err, ErrSSHKeyExists) {
		t.Fatalf("duplicate across users = %v", err)
	}
	_, bare := ed25519Line(t, "")
	named, err := s.Add("alice", strings.Repeat("名", 150), bare)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(named.Name)) != maxSSHKeyNameRunes {
		t.Fatalf("name not truncated: %d", len([]rune(named.Name)))
	}
	_, typeOnly := ed25519Line(t, "")
	if row, err := s.Add("alice", "", typeOnly); err != nil || row.Name != ssh.KeyAlgoED25519 {
		t.Fatalf("default name = %+v %v", row, err)
	}
	keys, err := s.List("alice")
	if err != nil || len(keys) != 3 || keys[0].ID != row.ID {
		t.Fatalf("list = %+v %v", keys, err)
	}
	if !s.HasKey("alice") || s.HasKey("bob") {
		t.Fatal("HasKey mismatch")
	}

	var revoked []string
	s.SetRevokeHook(func(fp string) { revoked = append(revoked, fp) })
	if err := s.Delete("bob", row.ID); !errors.Is(err, ErrSSHKeyNotFound) {
		t.Fatalf("deleting another user's key = %v", err)
	}
	if err := s.Delete("alice", row.ID); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 || revoked[0] != row.Fingerprint {
		t.Fatalf("revoke hook = %v", revoked)
	}
	if _, err := s.AuthorizeKey(key); err == nil {
		t.Fatal("deleted key still authorized")
	}
}

func TestSSHKeyValidation(t *testing.T) {
	s, _ := newSSHKeyFixture(t)
	for name, line := range map[string]string{
		"empty":     "",
		"garbage":   "not a key",
		"two lines": "ssh-ed25519 AAAA\nssh-ed25519 BBBB",
		"options":   `command="ls" ` + strings.TrimSpace(func() string { _, l := ed25519Line(t, ""); return l }()),
	} {
		if _, err := s.Add("alice", "", line); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := s.Add("alice", "", rsaLine(t, 1024)); err == nil || !strings.Contains(err.Error(), "2048") {
		t.Fatalf("short RSA = %v", err)
	}
	if _, err := s.Add("alice", "", rsaLine(t, 2048)); err != nil {
		t.Fatalf("2048-bit RSA rejected: %v", err)
	}
	signer := newCertSigner(t)
	if _, err := s.Add("alice", "", string(ssh.MarshalAuthorizedKey(signer))); err == nil {
		t.Fatal("certificates are not supported")
	}
}

func newCertSigner(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ssh.NewPublicKey(pub)
	ca, _ := ssh.NewSignerFromKey(priv)
	cert := &ssh.Certificate{Key: key, CertType: ssh.UserCert, ValidBefore: ssh.CertTimeInfinity}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestSSHKeyLimitPerUser(t *testing.T) {
	s, _ := newSSHKeyFixture(t)
	for i := 0; i < maxSSHKeysPerUser; i++ {
		_, line := ed25519Line(t, "")
		if _, err := s.Add("alice", "", line); err != nil {
			t.Fatal(err)
		}
	}
	_, line := ed25519Line(t, "")
	if _, err := s.Add("alice", "", line); !errors.Is(err, ErrSSHKeyLimit) {
		t.Fatalf("limit = %v", err)
	}
}

func TestSSHKeyAuthorize(t *testing.T) {
	s, cfg := newSSHKeyFixture(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	key, line := ed25519Line(t, "")
	if _, err := s.Add("alice", "laptop", line); err != nil {
		t.Fatal(err)
	}
	who, err := s.AuthorizeKey(key)
	if err != nil || who.Username != "alice" || who.Fingerprint != ssh.FingerprintSHA256(key) {
		t.Fatalf("authorize = %+v %v", who, err)
	}
	lastUsed := func() time.Time {
		var row models.UserSSHKey
		if err := s.db.First(&row, "fingerprint = ?", who.Fingerprint).Error; err != nil || row.LastUsedAt == nil {
			t.Fatalf("last used not recorded: %+v %v", row, err)
		}
		return *row.LastUsedAt
	}
	if !lastUsed().Equal(now) {
		t.Fatal("last used time wrong")
	}
	now = now.Add(10 * time.Second)
	_, _ = s.AuthorizeKey(key)
	if !lastUsed().Equal(now.Add(-10 * time.Second)) {
		t.Fatal("last used rewritten within the throttle window")
	}
	now = now.Add(time.Minute)
	_, _ = s.AuthorizeKey(key)
	if !lastUsed().Equal(now) {
		t.Fatal("last used not refreshed after the window")
	}

	other, _ := ed25519Line(t, "")
	if _, err := s.AuthorizeKey(other); err == nil {
		t.Fatal("unregistered key authorized")
	}

	cfg.SSHGateway.AdminsOnly = true
	if _, err := s.AuthorizeKey(key); err == nil {
		t.Fatal("admins_only let a regular user in")
	}
	adminKey, adminLine := ed25519Line(t, "")
	if _, err := s.Add("root-admin", "", adminLine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeKey(adminKey); err != nil {
		t.Fatalf("admin rejected: %v", err)
	}
	cfg.SSHGateway.AdminsOnly = false

	cfg.Auth.Users = cfg.Auth.Users[1:]
	if _, err := s.AuthorizeKey(key); err == nil {
		t.Fatal("key of a removed account authorized")
	}
	nilCfg := NewSSHKeyService(s.db, nil)
	if _, err := nilCfg.AuthorizeKey(adminKey); err == nil {
		t.Fatal("no config must authorize nobody")
	}
}
