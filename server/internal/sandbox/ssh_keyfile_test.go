package sandbox

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestDataPlaneKeyPath(t *testing.T) {
	if got := DataPlaneKeyPath("/var/lib/grasp/secrets.key"); got != "/var/lib/grasp/"+dataPlaneSSHKeyFileName {
		t.Fatalf("sqlite dir = %s", got)
	}
	if got := DataPlaneKeyPath("secrets.key"); got != dataPlaneSSHKeyFileName {
		t.Fatalf("relative secrets.key = %s", got)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := DataPlaneKeyPath(""); got != filepath.Join(wd, dataPlaneSSHKeyFileName) {
		t.Fatalf("empty rule = %s", got)
	}
	if got := DataPlaneKeyPath("  "); got != filepath.Join(wd, dataPlaneSSHKeyFileName) {
		t.Fatalf("blank rule = %s", got)
	}
}

func TestSSHKeyFileRoundTripLeavesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", dataPlaneSSHKeyFileName)

	prev := log.Logger
	var logs bytes.Buffer
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = prev })

	_, pub1, err := loadOrCreateSSHKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logs.Bytes(), []byte("PRIVATE KEY")) {
		t.Fatal("create log contains a private key")
	}

	_, pub2, err := loadOrCreateSSHKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if pub1 == "" || pub1 != pub2 {
		t.Fatal("reloaded public key differs")
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("reload rewrote the key file")
	}
	if !bytes.Contains(first, []byte("PRIVATE KEY")) {
		t.Fatal("key file is not a PEM private key")
	}

	const marker = "CORRUPT-KEY-MATERIAL-DO-NOT-LOG"
	corrupt := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n" + marker + "\n-----END OPENSSH PRIVATE KEY-----\n")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	_, _, err = loadOrCreateSSHKeyFile(path)
	if err == nil {
		t.Fatal("corrupt key file should fail")
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "PRIVATE KEY") || strings.Contains(err.Error(), "BEGIN ") {
		t.Fatal("corrupt key error contains key material")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Fatal("corrupt key file was overwritten")
	}
	logged := logs.String()
	if strings.Contains(logged, marker) || strings.Contains(logged, "PRIVATE KEY") || strings.Contains(logged, "BEGIN ") {
		t.Fatal("corrupt key log contains key material")
	}
	if !strings.Contains(logged, "not replacing") {
		t.Fatal("corrupt key was not reported")
	}
}
