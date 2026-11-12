package config

import (
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (c *Config) validate() error {
	if err := c.validateBackend(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Server.Listen) == "" {
		return fmt.Errorf("server.listen is required")
	}
	if c.Server.FinalizeTimeoutSeconds <= 0 || !validSeconds(c.Server.FinalizeTimeoutSeconds) {
		return fmt.Errorf("server.finalizeTimeoutSeconds must be positive and fit a duration")
	}
	if c.OrphanGC.IntervalSeconds <= 0 || c.OrphanGC.MinAgeSeconds < 0 ||
		!validSeconds(c.OrphanGC.IntervalSeconds) || !validSeconds(c.OrphanGC.MinAgeSeconds) {
		return fmt.Errorf("orphanGC requires a positive intervalSeconds and non-negative minAgeSeconds")
	}
	switch c.Database.Driver {
	case "sqlite", "mysql":
	default:
		return fmt.Errorf("database.driver must be sqlite or mysql")
	}
	if c.Database.Port < 0 || c.Database.Port > 65535 {
		return fmt.Errorf("database.port must be between 0 and 65535")
	}
	if c.E2B != nil {
		return c.validateE2B()
	}
	if strings.TrimSpace(c.DefaultImage()) == "" {
		return fmt.Errorf("%s.image is required", c.Driver)
	}
	for name, profile := range c.EnvironmentProfiles() {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(profile.Image) == "" {
			return fmt.Errorf("%s.profiles require a non-empty name and image", c.Driver)
		}
	}
	var resources ResourceConfig
	if c.Docker != nil {
		resources = c.Docker.Resources
	} else {
		resources = c.Kubernetes.Resources
	}
	for _, values := range []ResourceValues{resources.Default, resources.Max} {
		if values.CPUCores <= 0 || math.IsNaN(values.CPUCores) || math.IsInf(values.CPUCores, 0) || values.MemoryMB <= 0 || values.DiskGi <= 0 {
			return fmt.Errorf("%s.resources default and max require positive finite cpuCores, memoryMB and diskGi", c.Driver)
		}
	}
	if resources.Default.CPUCores > resources.Max.CPUCores || resources.Default.MemoryMB > resources.Max.MemoryMB || resources.Default.DiskGi > resources.Max.DiskGi {
		return fmt.Errorf("%s.resources.default cannot exceed resources.max", c.Driver)
	}
	if k := c.Kubernetes; k != nil {
		if k.CPURequestCores < 0 || math.IsNaN(k.CPURequestCores) || math.IsInf(k.CPURequestCores, 0) || k.MemoryRequestMB < 0 {
			return fmt.Errorf("kubernetes scheduling requests must be non-negative finite values")
		}
		for _, ratio := range []float64{k.CPURequestRatio, k.MemoryRequestRatio} {
			if math.IsNaN(ratio) || ratio <= 0 || ratio > 1 {
				return fmt.Errorf("kubernetes request ratios must be in (0, 1]")
			}
		}
		switch k.ImagePullPolicy {
		case "Always", "IfNotPresent", "Never":
		default:
			return fmt.Errorf("kubernetes.imagePullPolicy must be Always, IfNotPresent or Never")
		}
	}
	return nil
}

func validSeconds(value int) bool { return int64(value) <= math.MaxInt64/int64(time.Second) }

func (c *Config) validateE2B() error {
	e := c.E2B
	if strings.TrimSpace(e.APIKey) == "" || strings.ContainsAny(e.APIKey, "\r\n") {
		return fmt.Errorf("e2b.apiKey is required and must be a valid header value")
	}
	if strings.TrimSpace(e.OwnerID) == "" || strings.ContainsAny(e.OwnerID, "\r\n\x00") {
		return fmt.Errorf("e2b.ownerID is required and must be stable for this deployment")
	}
	if !filepath.IsAbs(e.StateDir) || filepath.Clean(e.StateDir) == "/" || strings.ContainsRune(e.StateDir, '\x00') {
		return fmt.Errorf("e2b.stateDir must be an explicit absolute persistent directory")
	}
	if e.APIURL == "" {
		return fmt.Errorf("e2b.apiURL is required; specify your self-hosted API or https://api.e2b.app")
	}
	for _, origin := range []string{e.APIURL, e.SandboxURL} {
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("e2b.apiURL and sandboxURL must be HTTP(S) origins without credentials, paths or query strings")
		}
		if port := u.Port(); port != "" {
			number, err := strconv.Atoi(port)
			if err != nil || number < 1 || number > 65535 {
				return fmt.Errorf("e2b origin port is invalid")
			}
		}
	}
	if e.BootTimeoutSeconds <= 0 || e.LeaseSeconds < 30 || e.RenewIntervalSeconds <= 0 ||
		!validSeconds(e.BootTimeoutSeconds) || !validSeconds(e.LeaseSeconds) || int64(e.RenewIntervalSeconds) >= (int64(e.LeaseSeconds)+1)/2 {
		return fmt.Errorf("e2b requires positive timeouts, leaseSeconds at least 30 and renewal before half the lease")
	}
	if len(e.Templates) == 0 {
		return fmt.Errorf("e2b.templates must contain prepared templates")
	}
	for name, template := range e.Templates {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name || strings.TrimSpace(template.TemplateID) == "" ||
			template.CPUCores <= 0 || math.IsNaN(template.CPUCores) || math.IsInf(template.CPUCores, 0) || math.Trunc(template.CPUCores) != template.CPUCores ||
			template.MemoryMB <= 0 || template.DiskGi <= 0 || template.DiskGi > 1<<30 {
			return fmt.Errorf("e2b.templates require non-empty names, explicit template IDs and positive whole CPUs/memoryMB/diskGi")
		}
	}
	if _, ok := e.Templates[e.DefaultTemplate]; !ok {
		return fmt.Errorf("e2b.defaultTemplate must select a configured template (required when there is more than one)")
	}
	return nil
}
