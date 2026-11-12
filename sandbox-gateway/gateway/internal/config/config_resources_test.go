package config

import (
	"testing"
)

func TestResolveResourcesDefaultsAndMax(t *testing.T) {
	d := ResourceDefaults{
		DefaultCPUCores: 2,
		DefaultMemoryMB: 4096,
		DefaultDiskGi:   25,
		MaxCPUCores:     8,
		MaxMemoryMB:     16384,
		MaxDiskGi:       500,
	}
	cpu, mem, disk, err := d.Resolve(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cpu != 2 || mem != 4096 || disk != 25 {
		t.Fatalf("defaults: got %v %v %v", cpu, mem, disk)
	}
	if _, _, _, err := d.Resolve(16, 0, 0); err == nil {
		t.Fatal("expected cpu max error")
	}
	if _, _, _, err := d.Resolve(2, 0, 600); err == nil {
		t.Fatal("expected disk max error")
	}
	if _, _, _, err := d.Resolve(2, 999999, 1); err == nil {
		t.Fatal("expected memory max error")
	}
	cpu, mem, disk, err = d.Resolve(4, 8192, 160)
	if err != nil {
		t.Fatal(err)
	}
	if cpu != 4 || mem != 8192 || disk != 160 {
		t.Fatalf("custom: got %v %v %v", cpu, mem, disk)
	}
}

func TestResolveRejectsNegativesAndEmptyDefaults(t *testing.T) {
	d := ResourceDefaults{} // zeros: after fill, values stay <= 0
	if _, _, _, err := d.Resolve(-1, 0, 0); err == nil {
		t.Fatal("negative cpu")
	}
	if _, _, _, err := d.Resolve(0, -1, 0); err == nil {
		t.Fatal("negative memory")
	}
	if _, _, _, err := d.Resolve(0, 0, -1); err == nil {
		t.Fatal("negative disk")
	}
	if _, _, _, err := d.Resolve(0, 0, 0); err == nil {
		t.Fatal("empty defaults must reject zero effective cpu")
	}
	d.DefaultCPUCores = 1
	if _, _, _, err := d.Resolve(0, 0, 0); err == nil {
		t.Fatal("empty memory/disk defaults")
	}
	d.DefaultMemoryMB = 512
	if _, _, _, err := d.Resolve(0, 0, 0); err == nil {
		t.Fatal("empty disk default")
	}
}
