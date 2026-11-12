package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"sandbox-gateway/internal/api"
	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/driver"
	dockerdriver "sandbox-gateway/internal/driver/docker"
	e2bdriver "sandbox-gateway/internal/driver/e2b"
	k8sdriver "sandbox-gateway/internal/driver/kubernetes"
	"sandbox-gateway/internal/logging"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"

	"github.com/rs/zerolog/log"
)

func main() {
	logging.Init("sandbox-gateway")

	configPath := flag.String("config", envOr("SBGW_CONFIG", "config.yaml"), "path to config YAML")
	healthcheck := flag.Bool("healthcheck", false, "check the configured local gateway and exit without initializing its driver")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal().Err(err).Str("config_path", *configPath).Msg("load config failed")
	}
	if *healthcheck {
		if err := checkGatewayHealth(context.Background(), cfg); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "gateway healthcheck failed")
			os.Exit(1)
		}
		return
	}

	drv, err := buildDriver(cfg)
	if err != nil {
		log.Fatal().Err(err).Str("driver", cfg.Driver).Msg("build driver failed")
	}
	log.Info().
		Str("driver", cfg.Driver).
		Str("db_driver", cfg.Database.Driver).
		Str("db_host", cfg.Database.Host).
		Str("db_name", cfg.Database.Name).
		Bool("db_dsn_set", cfg.Database.DSN != "").
		Str("image", cfg.Image.Ref).
		Str("listen", cfg.Server.Listen).
		Msg("gateway starting")

	db, err := database.Open(cfg.Database)
	if err != nil {
		log.Fatal().Err(err).Str("db_driver", cfg.Database.Driver).Msg("open database failed")
	}
	st := store.New(db)

	svc := service.New(drv, st, service.Config{
		Image:            cfg.Image.Ref,
		Ports:            cfg.Image.Ports.Public(),
		InternalPorts:    cfg.Image.Ports.Internal(),
		SessionPort:      cfg.Image.Ports.Session,
		FinalizeTimeout:  cfg.FinalizeTimeout(),
		Resources:        cfg.ResourceDefaults(),
		OrphanGCInterval: cfg.OrphanGCInterval(),
		OrphanGCMinAge:   cfg.OrphanGCMinAge(),
	})

	// Reconcile persisted records against live driver state on startup.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		svc.ReconcileOnStartup(ctx)
	}()

	if cfg.OrphanGC.Enabled {
		go svc.RunOrphanGC(context.Background())
	}

	h := api.NewHandler(svc, cfg.Image.Ports)
	r := api.NewRouter(h, cfg)
	if err := r.Run(cfg.Server.Listen); err != nil {
		log.Fatal().Err(err).Str("listen", cfg.Server.Listen).Msg("http server failed")
	}
}

// checkGatewayHealth deliberately does not initialize a driver or open its
// recovery journal. Compose can run it while the main process owns that lock
// and is restoring private sandbox endpoints before opening the HTTP listener.
func checkGatewayHealth(ctx context.Context, cfg *config.Config) error {
	host, port, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return errors.New("invalid local gateway listener")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("invalid local gateway listener port")
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		return errors.New("cannot construct local gateway healthcheck")
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("local gateway healthcheck request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("local gateway healthcheck status failed")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 {
		return errors.New("local gateway healthcheck response is invalid")
	}
	var result struct {
		Status string `json:"status"`
		Driver string `json:"driver"`
	}
	if json.Unmarshal(data, &result) != nil || result.Status != "ok" || result.Driver != cfg.Driver {
		return errors.New("local gateway healthcheck identity mismatch")
	}
	return nil
}

func buildDriver(cfg *config.Config) (driver.Driver, error) {
	switch cfg.Driver {
	case "docker":
		return dockerdriver.New(dockerdriver.Options{
			BindIP:        cfg.Docker.BindIP,
			Network:       cfg.Docker.Network,
			NamePrefix:    cfg.Docker.NamePrefix,
			ShmSize:       cfg.Docker.ShmSize,
			InternalPorts: cfg.Image.Ports.Internal(),
		}), nil
	case "kubernetes":
		return k8sdriver.New(k8sdriver.Options{
			InCluster:          cfg.K8s.InCluster,
			Kubeconfig:         cfg.K8s.Kubeconfig,
			Namespace:          cfg.K8s.Namespace,
			NamePrefix:         cfg.K8s.NamePrefix,
			StorageClass:       cfg.K8s.StorageClass,
			DataDiskGi:         cfg.K8s.DataDiskGi,
			PVCAnnotations:     cfg.K8s.PVCAnnotations,
			ImagePullSecret:    cfg.K8s.ImagePullSecret,
			ImagePullPolicy:    cfg.K8s.ImagePullPolicy,
			EnableLoadBalancer: cfg.K8s.EnableLoadBalancer,
			CPUCores:           cfg.K8s.CPUCores,
			MemoryMB:           cfg.K8s.MemoryMB,
			CPURequestCores:    cfg.K8s.CPURequestCores,
			MemoryRequestMB:    cfg.K8s.MemoryRequestMB,
			CPURequestRatio:    cfg.K8s.CPURequestRatio,
			MemoryRequestRatio: cfg.K8s.MemoryRequestRatio,
			PublicPorts:        cfg.Image.Ports.Public(),
			InternalPorts:      cfg.Image.Ports.Internal(),
		})
	case "e2b":
		profiles := make([]e2bdriver.TemplateProfile, 0, len(cfg.E2B.Templates))
		for _, template := range cfg.E2B.Templates {
			profiles = append(profiles, e2bdriver.TemplateProfile{
				Image: template.Image, TemplateID: template.TemplateID,
				CPUCores: template.CPUCores, MemoryMB: template.MemoryMB, DiskGi: template.DiskGi,
			})
		}
		return e2bdriver.New(e2bdriver.Options{
			ClientOptions: e2bdriver.ClientOptions{
				APIURL: cfg.E2B.APIURL, SandboxURL: cfg.E2B.SandboxURL,
				Domain: cfg.E2B.Domain, APIKey: cfg.E2B.APIKey,
			},
			OwnerID: cfg.E2B.OwnerID, StateDir: cfg.E2B.StateDir, Profiles: profiles,
			BootTimeout:   time.Duration(cfg.E2B.BootTimeoutSeconds) * time.Second,
			Lease:         time.Duration(cfg.E2B.LeaseSeconds) * time.Second,
			RenewInterval: time.Duration(cfg.E2B.RenewIntervalSeconds) * time.Second,
			AutoPause:     cfg.E2B.AutoPause,
		})
	default:
		return nil, fmt.Errorf("unknown driver %q", cfg.Driver)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
