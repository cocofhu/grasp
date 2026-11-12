package config

import (
	"fmt"
	"math"
)

type ResourceDefaults struct {
	DefaultCPUCores float64
	DefaultMemoryMB int64
	DefaultDiskGi   int64
	MaxCPUCores     float64
	MaxMemoryMB     int64
	MaxDiskGi       int64
}

// ResolveResources fills zeros with defaults and validates against max caps.
// Negative values are rejected. Returns the effective limits.
func (d ResourceDefaults) Resolve(cpuCores float64, memoryMB, diskGi int64) (float64, int64, int64, error) {
	if math.IsNaN(cpuCores) || math.IsInf(cpuCores, 0) || cpuCores < 0 {
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

// ResourcePolicy resolves a selected environment before filling omitted values.
// E2B resources must exactly match that template; container backends use caps.
type ResourcePolicy struct {
	Defaults  ResourceDefaults
	Templates map[string]E2BTemplateConfig
}

func (p ResourcePolicy) Resolve(image string, cpu float64, memory, disk int64) (float64, int64, int64, error) {
	if len(p.Templates) == 0 {
		return p.Defaults.Resolve(cpu, memory, disk)
	}
	template, ok := p.Templates[image]
	if !ok {
		return 0, 0, 0, fmt.Errorf("unknown E2B template %q", image)
	}
	if math.IsNaN(cpu) || math.IsInf(cpu, 0) || cpu < 0 || memory < 0 || disk < 0 {
		return 0, 0, 0, fmt.Errorf("E2B resources must be non-negative finite values")
	}
	if cpu == 0 {
		cpu = template.CPUCores
	}
	if memory == 0 {
		memory = template.MemoryMB
	}
	if disk == 0 {
		disk = template.DiskGi
	}
	if cpu != template.CPUCores || memory != template.MemoryMB || disk != template.DiskGi {
		return 0, 0, 0, fmt.Errorf("E2B template %q requires cpuCores=%g memoryMB=%d diskGi=%d; requested cpuCores=%g memoryMB=%d diskGi=%d", image, template.CPUCores, template.MemoryMB, template.DiskGi, cpu, memory, disk)
	}
	return cpu, memory, disk, nil
}

func (c *Config) ResourcePolicy() ResourcePolicy {
	if c.E2B != nil {
		return ResourcePolicy{Templates: c.E2B.Templates}
	}
	var resources ResourceConfig
	if c.Docker != nil {
		resources = c.Docker.Resources
	}
	if c.Kubernetes != nil {
		resources = c.Kubernetes.Resources
	}
	return ResourcePolicy{Defaults: ResourceDefaults{
		DefaultCPUCores: resources.Default.CPUCores, DefaultMemoryMB: resources.Default.MemoryMB, DefaultDiskGi: resources.Default.DiskGi,
		MaxCPUCores: resources.Max.CPUCores, MaxMemoryMB: resources.Max.MemoryMB, MaxDiskGi: resources.Max.DiskGi,
	}}
}
