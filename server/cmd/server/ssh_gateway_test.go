package main

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/services"
)

func TestStartSSHGateway(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(db)
	keys := services.NewSSHKeyService(db, config.GetConfig)
	sbx := &services.SandboxService{}

	srv, info, err := startSSHGateway(config.SSHGatewayConfig{}, keys, sbx)
	if srv != nil || info != nil || err != nil {
		t.Fatal("disabled gateway must not start")
	}

	dir := t.TempDir()
	cfg := config.SSHGatewayConfig{
		Enabled: true, Listen: "127.0.0.1:0", PublicHost: "ssh.example.com", PublicPort: 2222,
		HostKeyPath: filepath.Join(dir, "host_key"), MaxConns: 5, MaxConnsPerSandbox: 2, AdminsOnly: true,
	}
	srv, info, err = startSSHGateway(cfg, keys, sbx)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if info.PublicHost != "ssh.example.com" || info.PublicPort != 2222 || !info.AdminsOnly ||
		info.HostKeyFingerprint != srv.HostKeyFingerprint() {
		t.Fatalf("info = %+v", info)
	}

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	cfg.Listen = busy.Addr().String()
	if _, _, err := startSSHGateway(cfg, keys, sbx); err == nil {
		t.Fatal("an occupied port must fail startup")
	}
	cfg.HostKeyPath = filepath.Join(busy.Addr().String(), "\x00")
	if _, _, err := startSSHGateway(cfg, keys, sbx); err == nil {
		t.Fatal("an unusable host key path must fail startup")
	}
}
