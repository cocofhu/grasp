package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (c *Config) FinalizeTimeout() time.Duration {
	return time.Duration(c.Server.FinalizeTimeoutSeconds) * time.Second
}
func (c *Config) OrphanGCInterval() time.Duration {
	return time.Duration(c.OrphanGC.IntervalSeconds) * time.Second
}
func (c *Config) OrphanGCMinAge() time.Duration {
	return time.Duration(c.OrphanGC.MinAgeSeconds) * time.Second
}

func defaultResources() ResourceConfig {
	return ResourceConfig{Default: ResourceValues{CPUCores: 2, MemoryMB: 4096, DiskGi: 25}, Max: ResourceValues{CPUCores: 8, MemoryMB: 16384, DiskGi: 500}}
}

// Default is the Docker configuration used when no file or driver is selected.
func Default() *Config {
	c, _ := DefaultForDriver("docker")
	return c
}

func DefaultForDriver(driver string) (*Config, error) {
	c := &Config{
		Driver:   driver,
		Server:   ServerConfig{Listen: ":8080", FinalizeTimeoutSeconds: 1200},
		Database: DBConfig{Driver: "sqlite", Path: "gateway.db"},
		Ports: PortsConfig{Session: 8765, CodeServer: 8744, SSH: 22, CDP: 9222, NoVNC: 6080,
			App: []int{80, 443, 2222, 3000, 3306, 4200, 5000, 5173, 5432, 5500, 6379, 8000, 8080, 8443, 8888, 9090, 9200, 9229}},
		OrphanGC: OrphanGCConfig{IntervalSeconds: 300, MinAgeSeconds: 600},
	}
	switch driver {
	case "docker":
		c.Docker = &DockerConfig{Image: "universal-sandbox:local", BindIP: "127.0.0.1", NamePrefix: "sbx-", ShmSize: "1g", Resources: defaultResources()}
	case "kubernetes":
		c.Kubernetes = &KubernetesConfig{Image: "universal-sandbox:local", Namespace: "sandboxes", NamePrefix: "sbx-", ImagePullPolicy: "IfNotPresent", EnableLoadBalancer: true,
			Resources: defaultResources(), CPURequestCores: 0.25, MemoryRequestMB: 512, CPURequestRatio: 0.25, MemoryRequestRatio: 0.25}
	case "e2b":
		c.E2B = &E2BConfig{BootTimeoutSeconds: 600, LeaseSeconds: 3600, RenewIntervalSeconds: 300, AutoPause: true}
	default:
		return nil, fmt.Errorf("driver must be docker, kubernetes or e2b")
	}
	return c, nil
}

// Load accepts one YAML document and one backend. Unknown keys and inactive
// backend settings fail before the gateway can create or reconcile sandboxes.
func Load(path string) (*Config, error) {
	var data []byte
	if path != "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	var selector struct {
		Driver string `yaml:"driver"`
	}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &selector); err != nil {
			return nil, fmt.Errorf("parse gateway config: invalid YAML")
		}
	}
	driver := strings.TrimSpace(selector.Driver)
	if value := os.Getenv("SBGW_DRIVER"); value != "" {
		if driver != "" && driver != value {
			return nil, fmt.Errorf("SBGW_DRIVER conflicts with YAML driver; select one backend configuration")
		}
		driver = value
	}
	if driver == "" {
		driver = "docker"
	}
	c, err := DefaultForDriver(driver)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err = decoder.Decode(c); err != nil {
			return nil, fmt.Errorf("parse gateway config: unknown field or invalid value; check the selected backend schema")
		}
		var trailing any
		if err = decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("gateway config must contain exactly one YAML document")
		}
	}
	if c.Driver == "" {
		c.Driver = driver
	}
	if err = c.validateBackend(); err != nil {
		return nil, err
	}
	if err = applyEnv(c); err != nil {
		return nil, err
	}
	if c.E2B != nil && c.E2B.DefaultTemplate == "" && len(c.E2B.Templates) == 1 {
		for name := range c.E2B.Templates {
			c.E2B.DefaultTemplate = name
		}
	}
	if err = c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validateBackend() error {
	active := 0
	if c.Docker != nil {
		active++
		if c.Driver != "docker" {
			return fmt.Errorf("docker configuration requires driver: docker")
		}
	}
	if c.Kubernetes != nil {
		active++
		if c.Driver != "kubernetes" {
			return fmt.Errorf("kubernetes configuration requires driver: kubernetes")
		}
	}
	if c.E2B != nil {
		active++
		if c.Driver != "e2b" {
			return fmt.Errorf("e2b configuration requires driver: e2b")
		}
	}
	if active != 1 {
		return fmt.Errorf("configure exactly one backend matching driver")
	}
	return nil
}

func (c *Config) DefaultImage() string {
	switch c.Driver {
	case "docker":
		return c.Docker.Image
	case "kubernetes":
		return c.Kubernetes.Image
	case "e2b":
		return c.E2B.DefaultTemplate
	}
	return ""
}

func (c *Config) EnvironmentProfiles() map[string]ProfileConfig {
	switch c.Driver {
	case "docker":
		return c.Docker.Profiles
	case "kubernetes":
		return c.Kubernetes.Profiles
	case "e2b":
		profiles := make(map[string]ProfileConfig, len(c.E2B.Templates))
		for name := range c.E2B.Templates {
			profiles[name] = ProfileConfig{Image: name}
		}
		return profiles
	}
	return nil
}

func (c *Config) TemplateNames() []string {
	if c.E2B == nil {
		return nil
	}
	names := make([]string, 0, len(c.E2B.Templates))
	for name := range c.E2B.Templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
