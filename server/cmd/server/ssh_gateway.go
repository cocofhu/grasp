package main

import (
	"net"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/sshgateway"
	"github.com/rs/zerolog/log"
)

// startSSHGateway serves the SSH gateway when enabled. Settings are read once
// at startup; changing them needs a restart.
func startSSHGateway(c config.SSHGatewayConfig, keys *services.SSHKeyService, sbx *services.SandboxService) (*sshgateway.Server, *handlers.SSHGatewayInfo, error) {
	if !c.Enabled {
		return nil, nil, nil
	}
	srv, err := sshgateway.New(sshgateway.Options{
		HostKeyPath:        c.HostKeyPath,
		MaxConns:           c.MaxConns,
		MaxConnsPerSandbox: c.MaxConnsPerSandbox,
	}, keys, sbx)
	if err != nil {
		return nil, nil, err
	}
	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		_ = srv.Close()
		return nil, nil, err
	}
	keys.SetRevokeHook(func(fingerprint string) { srv.DisconnectFingerprint(fingerprint) })
	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Error().Err(err).Msg("ssh gateway stopped")
		}
	}()
	log.Info().Str("listen", c.Listen).Str("public_host", c.PublicHost).Int("public_port", c.PublicPort).
		Str("host_key", srv.HostKeyFingerprint()).Bool("admins_only", c.AdminsOnly).Msg("ssh gateway listening")
	return srv, &handlers.SSHGatewayInfo{
		PublicHost:         c.PublicHost,
		PublicPort:         c.PublicPort,
		HostKeyFingerprint: srv.HostKeyFingerprint(),
		AdminsOnly:         c.AdminsOnly,
	}, nil
}
