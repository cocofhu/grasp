package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FinalizeTimeout returns the async-readiness bound (LB IP + session-port probe)
// as a duration.
func (c *Config) FinalizeTimeout() time.Duration {
	return time.Duration(c.Server.FinalizeTimeoutSeconds) * time.Second
}

// Config is the gateway's runtime configuration. It is loaded from a YAML file
// and then overlaid with SBGW_* environment variables (env wins). A gateway
// instance runs exactly one driver (Driver): docker, kubernetes, or e2b.
type Config struct {
	Server   ServerConfig `yaml:"server"`
	Driver   string       `yaml:"driver"` // "docker" | "kubernetes" | "e2b"
	Database DBConfig     `yaml:"database"`
	Image    ImageConfig  `yaml:"image"`
	// Profiles map stable environment names to this driver's approved image
	// identities. E2B resolves those identities to its prebuilt templates.
	Profiles map[string]ProfileConfig `yaml:"profiles"`
	Docker   DockerConfig             `yaml:"docker"`
	K8s      K8sConfig                `yaml:"kubernetes"`
	E2B      E2BConfig                `yaml:"e2b"`
	Auth     AuthConfig               `yaml:"auth"`
	OrphanGC OrphanGCConfig           `yaml:"orphanGC"`
}

type ProfileConfig struct {
	Image string `yaml:"image" json:"image"`
}

// OrphanGCConfig controls periodic cleanup of driver workloads that have no
// matching control-plane record (e.g. left behind after DB loss or failed DELETE).
type OrphanGCConfig struct {
	Enabled bool `yaml:"enabled"`
	// IntervalSeconds is the sweep period. Default 300 (5m) when enabled.
	IntervalSeconds int `yaml:"intervalSeconds"`
	// MinAgeSeconds skips workloads younger than this (avoids racing create /
	// read-replica lag). Default 600 (10m) when enabled. 0 disables the age gate.
	MinAgeSeconds int `yaml:"minAgeSeconds"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"` // e.g. ":8080"
	// FinalizeTimeoutSeconds bounds async readiness (LB IP + session-port probe)
	// after Create/Reinstall. Cold starts (large image pull + PVC attach + boot)
	// can exceed the old hard-coded 5m; default 1200 (20m). Must stay >= clients'
	// own create-wait timeout, else the client gives up before provisioning ends.
	FinalizeTimeoutSeconds int `yaml:"finalizeTimeoutSeconds"`
}

type DBConfig struct {
	// Driver is "sqlite" (local testing) or "mysql" (production). Default sqlite.
	Driver string `yaml:"driver"`
	// Path is the SQLite file path (driver=sqlite).
	Path string `yaml:"path"`
	// MySQL fields (driver=mysql). Prefer DSN when set; otherwise build from parts.
	DSN      string `yaml:"dsn"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`   // database name
	Params   string `yaml:"params"` // DSN query string; default charset=utf8mb4&parseTime=True&loc=Local
}

// ImageConfig captures the sandbox image reference and the container ports it
// exposes. Defaults align with the Phase 1 universal image
// (EXPOSE 8744 22 8765 9222 6080).
//
// The published sandbox is one universal-sandbox image. Resolve order:
//  1. explicit per-request image override,
//  2. Ref (the default/fallback image).
type ImageConfig struct {
	Ref   string      `yaml:"ref"`
	Ports PortsConfig `yaml:"ports"`
}

// Resolve returns the image ref for a create request: an explicit override
// wins, otherwise Ref.
func (i ImageConfig) Resolve(override, _ string) string {
	if override != "" {
		return override
	}
	return i.Ref
}

type PortsConfig struct {
	Session    int   `yaml:"session"`    // ACP bridge / WSP session, default 8765
	CodeServer int   `yaml:"codeServer"` // code-server, default 8744
	SSH        int   `yaml:"ssh"`        // sshd, default 22
	CDP        int   `yaml:"cdp"`        // Chromium CDP, default 9222 (internal-only)
	NoVNC      int   `yaml:"novnc"`      // noVNC websockify, default 6080 (internal-only)
	App        []int `yaml:"app"`        // extra user application ports to expose
}

func collectPorts(vals ...int) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, v := range vals {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Public returns ports that may be published to untrusted networks
// (Docker -p / K8s LoadBalancer): session, IDE, SSH, and extra app ports.
// CDP and noVNC are intentionally omitted — they have no application-layer auth.
func (p PortsConfig) Public() []int {
	vals := []int{p.Session, p.CodeServer, p.SSH}
	vals = append(vals, p.App...)
	return collectPorts(vals...)
}

// Internal returns ports that stay on the container / cluster network only
// (CDP 9222, noVNC 6080). Approving dials these in-cluster; they must not be
// published to the host or an external LoadBalancer.
func (p PortsConfig) Internal() []int {
	return collectPorts(p.CDP, p.NoVNC)
}

// All returns the external publish set. Historically this included CDP/noVNC;
// those are now Internal-only. Callers that publish to untrusted networks
// MUST use Public()/All(), not Internal().
func (p PortsConfig) All() []int {
	return p.Public()
}

// Listen returns Public ∪ Internal — every port the sandbox process listens on
// (ClusterIP / container ports). Not the external publish set.
func (p PortsConfig) Listen() []int {
	return collectPorts(append(p.Public(), p.Internal()...)...)
}

// DockerConfig configures the local Docker driver.
type DockerConfig struct {
	// BindIP is the host address docker publishes sandbox ports on. It MUST be
	// reachable by clients (not 127.0.0.1 if clients are remote). Default
	// "127.0.0.1" for local testing.
	BindIP string `yaml:"bindIP"`
	// Network is an optional docker network to attach sandboxes to.
	Network string `yaml:"network"`
	// NamePrefix prefixes all sandbox container names (list/reconcile filter).
	NamePrefix string `yaml:"namePrefix"`
	// ShmSize sets --shm-size (default "1g").
	ShmSize string `yaml:"shmSize"`
}

// E2BConfig maps approved sandbox image identities to prebuilt E2B templates.
// CPU and memory belong to the template, not the create request. Templates are
// explicit YAML configuration; there is no inferred or public base-template fallback.
type E2BConfig struct {
	APIURL               string `yaml:"apiURL"`
	SandboxURL           string `yaml:"sandboxURL"`
	Domain               string `yaml:"domain"`
	APIKey               string `yaml:"apiKey" json:"-"`
	OwnerID              string `yaml:"ownerID"`
	StateDir             string `yaml:"stateDir"`
	BootTimeoutSeconds   int    `yaml:"bootTimeoutSeconds"`
	LeaseSeconds         int    `yaml:"leaseSeconds"`
	RenewIntervalSeconds int    `yaml:"renewIntervalSeconds"`
	// AutoPause makes the provider pause with a memory snapshot, instead of
	// kill, when a guest reaches its lease or the provider's maximum lifetime.
	AutoPause bool                `yaml:"autoPause"`
	Templates []E2BTemplateConfig `yaml:"templates"`
}

type E2BTemplateConfig struct {
	Image      string  `yaml:"image"`
	TemplateID string  `yaml:"templateID"`
	CPUCores   float64 `yaml:"cpuCores"`
	MemoryMB   int64   `yaml:"memoryMB"`
	// DiskGi is minimum initially available workspace capacity verified in the
	// guest, not a resizable per-create PVC or a disk-usage quota.
	DiskGi int64 `yaml:"diskGi"`
}

// K8sConfig configures the production Kubernetes driver and default/max
// resource limits applied when creating sandboxes (per-request overrides).
type K8sConfig struct {
	InCluster          bool              `yaml:"inCluster"`
	Kubeconfig         string            `yaml:"kubeconfig"`
	Namespace          string            `yaml:"namespace"`  // shared namespace for sandboxes
	NamePrefix         string            `yaml:"namePrefix"` // e.g. "sbx-"
	StorageClass       string            `yaml:"storageClass"`
	DataDiskGi         int64             `yaml:"dataDiskGi"`     // default PVC size (GiB)
	MaxDataDiskGi      int64             `yaml:"maxDataDiskGi"`  // max PVC size per sandbox
	PVCAnnotations     map[string]string `yaml:"pvcAnnotations"` // applied to new sandbox data PVCs
	ImagePullSecret    string            `yaml:"imagePullSecret"`
	ImagePullPolicy    string            `yaml:"imagePullPolicy"`    // default "Always"
	EnableLoadBalancer bool              `yaml:"enableLoadBalancer"` // MetalLB LoadBalancer Service
	CPUCores           float64           `yaml:"cpuCores"`           // default CPU limit (cores)
	MaxCPUCores        float64           `yaml:"maxCPUCores"`        // max CPU limit per sandbox
	MemoryMB           int64             `yaml:"memoryMB"`           // default memory limit (MiB)
	MaxMemoryMB        int64             `yaml:"maxMemoryMB"`        // max memory limit per sandbox
	CPURequestCores    float64           `yaml:"cpuRequestCores"`    // fixed CPU request; >0 preferred over ratio
	MemoryRequestMB    int64             `yaml:"memoryRequestMB"`    // fixed memory request (MiB); >0 preferred over ratio
	CPURequestRatio    float64           `yaml:"cpuRequestRatio"`    // request = limit × ratio when fixed is 0
	MemoryRequestRatio float64           `yaml:"memoryRequestRatio"` // request = limit × ratio when fixed is 0
}

// AuthConfig configures control-plane authentication.
type AuthConfig struct {
	// APIKeys are accepted as `Authorization: Bearer <key>`. Empty disables
	// authentication (local testing only).
	APIKeys []string `yaml:"apiKeys"`
}

// ResourceDefaults is the resolved default/max resource policy used when
// creating sandboxes (shared by docker and kubernetes deployments).
type ResourceDefaults struct {
	DefaultCPUCores float64
	DefaultMemoryMB int64
	DefaultDiskGi   int64
	MaxCPUCores     float64
	MaxMemoryMB     int64
	MaxDiskGi       int64
}

// ResourceDefaults returns the sandbox resource policy from kubernetes config
// (also used as defaults for the docker driver).
func (c *Config) ResourceDefaults() ResourceDefaults {
	if c.Driver == "e2b" {
		result := ResourceDefaults{}
		for _, template := range c.E2B.Templates {
			if template.Image == c.Image.Ref {
				result.DefaultCPUCores = template.CPUCores
				result.DefaultMemoryMB = template.MemoryMB
				result.DefaultDiskGi = template.DiskGi
			}
			result.MaxCPUCores = math.Max(result.MaxCPUCores, template.CPUCores)
			if template.MemoryMB > result.MaxMemoryMB {
				result.MaxMemoryMB = template.MemoryMB
			}
			if template.DiskGi > result.MaxDiskGi {
				result.MaxDiskGi = template.DiskGi
			}
		}
		return result
	}
	return ResourceDefaults{
		DefaultCPUCores: c.K8s.CPUCores,
		DefaultMemoryMB: c.K8s.MemoryMB,
		DefaultDiskGi:   c.K8s.DataDiskGi,
		MaxCPUCores:     c.K8s.MaxCPUCores,
		MaxMemoryMB:     c.K8s.MaxMemoryMB,
		MaxDiskGi:       c.K8s.MaxDataDiskGi,
	}
}

// ResolveResources fills zeros with defaults and validates against max caps.
// Negative values are rejected. Returns the effective limits.
func (d ResourceDefaults) Resolve(cpuCores float64, memoryMB, diskGi int64) (float64, int64, int64, error) {
	if cpuCores < 0 {
		return 0, 0, 0, fmt.Errorf("cpuCores must be greater than 0")
	}
	if memoryMB < 0 {
		return 0, 0, 0, fmt.Errorf("memoryMB must be greater than 0")
	}
	if diskGi < 0 {
		return 0, 0, 0, fmt.Errorf("diskGi must be greater than 0")
	}
	if cpuCores == 0 {
		cpuCores = d.DefaultCPUCores
	}
	if memoryMB == 0 {
		memoryMB = d.DefaultMemoryMB
	}
	if diskGi == 0 {
		diskGi = d.DefaultDiskGi
	}
	if cpuCores <= 0 {
		return 0, 0, 0, fmt.Errorf("cpuCores must be greater than 0")
	}
	if memoryMB <= 0 {
		return 0, 0, 0, fmt.Errorf("memoryMB must be greater than 0")
	}
	if diskGi <= 0 {
		return 0, 0, 0, fmt.Errorf("diskGi must be greater than 0")
	}
	if d.MaxCPUCores > 0 && cpuCores > d.MaxCPUCores {
		return 0, 0, 0, fmt.Errorf("cpuCores cannot exceed %.2f", d.MaxCPUCores)
	}
	if d.MaxMemoryMB > 0 && memoryMB > d.MaxMemoryMB {
		return 0, 0, 0, fmt.Errorf("memoryMB cannot exceed %d", d.MaxMemoryMB)
	}
	if d.MaxDiskGi > 0 && diskGi > d.MaxDiskGi {
		return 0, 0, 0, fmt.Errorf("diskGi cannot exceed %d", d.MaxDiskGi)
	}
	return cpuCores, memoryMB, diskGi, nil
}

// SandboxCPURequest computes the scheduling CPU request from a limit: prefer
// fixed cpuRequestCores, otherwise limit × cpuRequestRatio.
func (c *Config) SandboxCPURequest(limitCores float64) float64 {
	if limitCores <= 0 {
		return limitCores
	}
	const minRequest = 0.05
	if fixed := c.K8s.CPURequestCores; fixed > 0 {
		request := fixed
		if request < minRequest {
			request = minRequest
		}
		if request > limitCores {
			return limitCores
		}
		return request
	}
	ratio := c.K8s.CPURequestRatio
	if ratio <= 0 {
		ratio = 0.25
	}
	if ratio > 1 {
		ratio = 1
	}
	request := limitCores * ratio
	if request < minRequest {
		return minRequest
	}
	if request > limitCores {
		return limitCores
	}
	return request
}

// SandboxMemoryRequest computes the scheduling memory request (MiB) from a
// limit: prefer fixed memoryRequestMB, otherwise limit × memoryRequestRatio.
func (c *Config) SandboxMemoryRequest(limitMB int64) int64 {
	if limitMB <= 0 {
		return limitMB
	}
	const minRequest int64 = 128
	if fixed := c.K8s.MemoryRequestMB; fixed > 0 {
		request := fixed
		if request < minRequest {
			request = minRequest
		}
		if request > limitMB {
			return limitMB
		}
		return request
	}
	ratio := c.K8s.MemoryRequestRatio
	if ratio <= 0 {
		ratio = 0.25
	}
	if ratio > 1 {
		ratio = 1
	}
	request := int64(float64(limitMB) * ratio)
	if request < minRequest {
		return minRequest
	}
	if request > limitMB {
		return limitMB
	}
	return request
}

// Default returns a config with sane defaults (Docker driver, local testing).
func Default() *Config {
	return &Config{
		Server:   ServerConfig{Listen: ":8080"},
		Driver:   "docker",
		Database: DBConfig{Driver: "sqlite", Path: "gateway.db"},
		Image: ImageConfig{
			Ref: "universal-sandbox:local",
			Ports: PortsConfig{
				Session:    8765,
				CodeServer: 8744,
				SSH:        22,
				CDP:        9222,
				NoVNC:      6080,
				// Extra app ports aligned with remote-dev sandboxLBPorts().
				App: []int{80, 443, 2222, 3000, 3306, 4200, 5000, 5173, 5432, 5500, 6379, 8000, 8080, 8443, 8888, 9090, 9200, 9229},
			},
		},
		Docker: DockerConfig{
			BindIP:     "127.0.0.1",
			NamePrefix: "sbx-",
			ShmSize:    "1g",
		},
		E2B: E2BConfig{
			Domain:               "e2b.app",
			BootTimeoutSeconds:   600,
			LeaseSeconds:         3600,
			RenewIntervalSeconds: 300,
			AutoPause:            true,
		},
		K8s: K8sConfig{
			Namespace:          "sandboxes",
			NamePrefix:         "sbx-",
			StorageClass:       "ugreen-iscsi",
			DataDiskGi:         25,
			MaxDataDiskGi:      500,
			ImagePullPolicy:    "IfNotPresent",
			EnableLoadBalancer: true,
			CPUCores:           2,
			MaxCPUCores:        8,
			MemoryMB:           4096,
			MaxMemoryMB:        16384,
			CPURequestCores:    0.25,
			MemoryRequestMB:    512,
			CPURequestRatio:    0.25,
			MemoryRequestRatio: 0.25,
		},
		OrphanGC: OrphanGCConfig{
			Enabled:         false,
			IntervalSeconds: 300,
			MinAgeSeconds:   600,
		},
	}
}

// Load reads config from path (if non-empty and present) then overlays SBGW_*
// environment variables. A missing file is not an error; defaults apply.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse config %s: %w", path, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	applyEnv(cfg)
	normalizeResourcePolicy(cfg)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func normalizeResourcePolicy(c *Config) {
	if strings.TrimSpace(c.Database.Driver) == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Server.FinalizeTimeoutSeconds <= 0 {
		c.Server.FinalizeTimeoutSeconds = 1200
	}
	if c.OrphanGC.IntervalSeconds <= 0 {
		c.OrphanGC.IntervalSeconds = 300
	}
	if c.OrphanGC.MinAgeSeconds < 0 {
		c.OrphanGC.MinAgeSeconds = 0
	}
	if strings.TrimSpace(c.K8s.ImagePullPolicy) == "" {
		c.K8s.ImagePullPolicy = "IfNotPresent"
	}
	if c.K8s.StorageClass == "" {
		c.K8s.StorageClass = "ugreen-iscsi"
	}
	if c.K8s.CPUCores <= 0 {
		c.K8s.CPUCores = 2
	}
	if c.K8s.MemoryMB <= 0 {
		c.K8s.MemoryMB = 4096
	}
	if c.K8s.DataDiskGi <= 0 {
		c.K8s.DataDiskGi = 25
	}
	if c.K8s.MaxCPUCores <= 0 {
		c.K8s.MaxCPUCores = 8
	}
	if c.K8s.MaxMemoryMB <= 0 {
		c.K8s.MaxMemoryMB = 16384
	}
	if c.K8s.MaxDataDiskGi <= 0 {
		c.K8s.MaxDataDiskGi = 500
	}
	if c.K8s.CPURequestRatio <= 0 {
		c.K8s.CPURequestRatio = 0.25
	}
	if c.K8s.CPURequestRatio > 1 {
		c.K8s.CPURequestRatio = 1
	}
	if c.K8s.MemoryRequestRatio <= 0 {
		c.K8s.MemoryRequestRatio = 0.25
	}
	if c.K8s.MemoryRequestRatio > 1 {
		c.K8s.MemoryRequestRatio = 1
	}
	if c.K8s.CPURequestCores == 0 {
		c.K8s.CPURequestCores = 0.25
	}
	if c.K8s.MemoryRequestMB == 0 {
		c.K8s.MemoryRequestMB = 512
	}
}

func (c *Config) validate() error {
	switch c.Driver {
	case "docker", "kubernetes", "e2b":
	default:
		return fmt.Errorf("invalid driver %q (want docker|kubernetes|e2b)", c.Driver)
	}
	if c.Image.Ref == "" {
		return fmt.Errorf("image.ref is required")
	}
	for name, profile := range c.Profiles {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(profile.Image) == "" {
			return fmt.Errorf("profiles require a non-empty name and image")
		}
	}
	if c.Driver == "e2b" {
		if err := c.validateE2B(); err != nil {
			return err
		}
	}
	switch strings.ToLower(strings.TrimSpace(c.Database.Driver)) {
	case "", "sqlite", "mysql":
	default:
		return fmt.Errorf("invalid database.driver %q (want sqlite|mysql)", c.Database.Driver)
	}
	return nil
}

func (c *Config) validateE2B() error {
	if strings.TrimSpace(c.E2B.APIKey) == "" || strings.ContainsAny(c.E2B.APIKey, "\r\n") {
		return fmt.Errorf("e2b.apiKey is required and must be a valid header value")
	}
	if strings.TrimSpace(c.E2B.OwnerID) == "" || strings.ContainsAny(c.E2B.OwnerID, "\r\n\x00") {
		return fmt.Errorf("e2b.ownerID is required and must be stable for this deployment")
	}
	if !filepath.IsAbs(c.E2B.StateDir) || filepath.Clean(c.E2B.StateDir) == "/" || strings.ContainsRune(c.E2B.StateDir, '\x00') {
		return fmt.Errorf("e2b.stateDir must be an explicit absolute persistent directory")
	}
	for _, origin := range []string{c.E2B.APIURL, c.E2B.SandboxURL} {
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
			u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("e2b origins must be HTTP(S) origins without credentials, paths or query strings")
		}
		if port := u.Port(); port != "" {
			number, err := strconv.Atoi(port)
			if err != nil || number < 1 || number > 65535 {
				return fmt.Errorf("e2b origin port is invalid")
			}
		}
	}
	if c.E2B.BootTimeoutSeconds <= 0 || c.E2B.LeaseSeconds < 30 || c.E2B.RenewIntervalSeconds <= 0 ||
		int64(c.E2B.RenewIntervalSeconds) >= (int64(c.E2B.LeaseSeconds)+1)/2 ||
		int64(c.E2B.BootTimeoutSeconds) > math.MaxInt64/int64(time.Second) ||
		int64(c.E2B.LeaseSeconds) > math.MaxInt64/int64(time.Second) {
		return fmt.Errorf("e2b requires positive timeouts, leaseSeconds at least 30 and renewal before half the lease")
	}
	if len(c.E2B.Templates) == 0 {
		return fmt.Errorf("e2b.templates must explicitly map image identities to prepared templates")
	}
	seen := make(map[string]bool)
	defaultFound := false
	for _, template := range c.E2B.Templates {
		if strings.TrimSpace(template.Image) == "" || strings.TrimSpace(template.TemplateID) == "" ||
			seen[template.Image] || template.CPUCores <= 0 || math.IsNaN(template.CPUCores) ||
			math.IsInf(template.CPUCores, 0) || math.Trunc(template.CPUCores) != template.CPUCores ||
			template.MemoryMB <= 0 || template.DiskGi <= 0 || template.DiskGi > 1<<30 {
			return fmt.Errorf("e2b.templates require unique images, explicit template IDs and positive whole CPUs/memoryMB/diskGi")
		}
		seen[template.Image] = true
		defaultFound = defaultFound || template.Image == c.Image.Ref
	}
	if !defaultFound {
		return fmt.Errorf("image.ref must match an explicit e2b.templates image")
	}
	return nil
}

// applyEnv overlays a curated set of SBGW_* variables. Only the most common
// knobs are exposed; the YAML file remains the source of truth for the rest.
func applyEnv(c *Config) {
	if v := os.Getenv("SBGW_LISTEN"); v != "" {
		c.Server.Listen = v
	}
	if v := os.Getenv("SBGW_FINALIZE_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Server.FinalizeTimeoutSeconds = n
		}
	}
	if v := os.Getenv("SBGW_DRIVER"); v != "" {
		c.Driver = v
	}
	if v := os.Getenv("SBGW_DB_DRIVER"); v != "" {
		c.Database.Driver = v
	}
	if v := os.Getenv("SBGW_DB_PATH"); v != "" {
		c.Database.Path = v
	}
	if v := os.Getenv("SBGW_DB_DSN"); v != "" {
		c.Database.DSN = v
	}
	if v := os.Getenv("SBGW_DB_HOST"); v != "" {
		c.Database.Host = v
	}
	if v := os.Getenv("SBGW_DB_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.Port = n
		}
	}
	if v := os.Getenv("SBGW_DB_USER"); v != "" {
		c.Database.User = v
	}
	if v := os.Getenv("SBGW_DB_PASSWORD"); v != "" {
		c.Database.Password = v
	}
	if v := os.Getenv("SBGW_DB_NAME"); v != "" {
		c.Database.Name = v
	}
	if v := os.Getenv("SBGW_IMAGE"); v != "" {
		c.Image.Ref = v
	}
	if v := os.Getenv("SBGW_API_KEYS"); v != "" {
		c.Auth.APIKeys = splitCSV(v)
	}
	// Docker
	if v := os.Getenv("SBGW_DOCKER_BIND_IP"); v != "" {
		c.Docker.BindIP = v
	}
	if v := os.Getenv("SBGW_DOCKER_NETWORK"); v != "" {
		c.Docker.Network = v
	}
	// E2B. Template/resource profiles are deliberately YAML-only.
	for key, destination := range map[string]*string{
		"SBGW_E2B_API_KEY": &c.E2B.APIKey, "SBGW_E2B_API_URL": &c.E2B.APIURL,
		"SBGW_E2B_SANDBOX_URL": &c.E2B.SandboxURL, "SBGW_E2B_DOMAIN": &c.E2B.Domain,
		"SBGW_E2B_OWNER_ID": &c.E2B.OwnerID, "SBGW_E2B_STATE_DIR": &c.E2B.StateDir,
	} {
		if value := os.Getenv(key); value != "" {
			*destination = value
		}
	}
	for key, destination := range map[string]*int{
		"SBGW_E2B_BOOT_TIMEOUT_SEC":   &c.E2B.BootTimeoutSeconds,
		"SBGW_E2B_LEASE_SEC":          &c.E2B.LeaseSeconds,
		"SBGW_E2B_RENEW_INTERVAL_SEC": &c.E2B.RenewIntervalSeconds,
	} {
		if value := os.Getenv(key); value != "" {
			if number, err := strconv.Atoi(value); err == nil {
				*destination = number
			} else {
				*destination = 0 // validateE2B rejects malformed E2B duration overrides.
			}
		}
	}
	if v := os.Getenv("SBGW_E2B_AUTO_PAUSE"); v != "" {
		c.E2B.AutoPause = parseBool(v, c.E2B.AutoPause)
	}
	// Kubernetes
	if v := os.Getenv("SBGW_K8S_IN_CLUSTER"); v != "" {
		c.K8s.InCluster = parseBool(v, c.K8s.InCluster)
	}
	if v := os.Getenv("SBGW_K8S_KUBECONFIG"); v != "" {
		c.K8s.Kubeconfig = v
	}
	if v := os.Getenv("SBGW_K8S_NAMESPACE"); v != "" {
		c.K8s.Namespace = v
	}
	if v := os.Getenv("SBGW_K8S_ENABLE_LB"); v != "" {
		c.K8s.EnableLoadBalancer = parseBool(v, c.K8s.EnableLoadBalancer)
	}
	if v := os.Getenv("SBGW_K8S_STORAGE_CLASS"); v != "" {
		c.K8s.StorageClass = v
	}
	if v := os.Getenv("SBGW_K8S_IMAGE_PULL_POLICY"); v != "" {
		c.K8s.ImagePullPolicy = v
	}
	if v := os.Getenv("SBGW_ORPHAN_GC_ENABLED"); v != "" {
		c.OrphanGC.Enabled = parseBool(v, c.OrphanGC.Enabled)
	}
	if v := os.Getenv("SBGW_ORPHAN_GC_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.OrphanGC.IntervalSeconds = n
		}
	}
	if v := os.Getenv("SBGW_ORPHAN_GC_MIN_AGE_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.OrphanGC.MinAgeSeconds = n
		}
	}
}

// OrphanGCInterval returns the sweep period.
func (c *Config) OrphanGCInterval() time.Duration {
	return time.Duration(c.OrphanGC.IntervalSeconds) * time.Second
}

// OrphanGCMinAge returns the minimum workload age before orphan deletion.
func (c *Config) OrphanGCMinAge() time.Duration {
	return time.Duration(c.OrphanGC.MinAgeSeconds) * time.Second
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseBool(v string, def bool) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}
