package config

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WorkspaceOperationsConfig controls bounded file copies and isolated restore
// directories. It never enables process-memory/volume-atomic checkpoints.
type WorkspaceOperationsConfig struct {
	Enabled               *bool  `yaml:"enabled"`
	PrivateRoot           string `yaml:"private_root"`
	MaxFileBytes          int64  `yaml:"max_file_bytes"`
	MaxUncompressedBytes  int64  `yaml:"max_uncompressed_bytes"`
	MaxGitBytes           int64  `yaml:"max_git_bytes"`
	MaxArchiveBytes       int64  `yaml:"max_archive_bytes"`
	MaxManifestBytes      int64  `yaml:"max_manifest_bytes"`
	MaxSpoolBytes         int64  `yaml:"max_spool_bytes"`
	MaxEntries            int64  `yaml:"max_entries"`
	MaxReservedBytes      int64  `yaml:"max_reserved_bytes"`
	MaxScopeReservedBytes int64  `yaml:"max_scope_reserved_bytes"`
	MaxDuration           string `yaml:"max_duration"`
	MaxWorkers            int    `yaml:"max_workers"`
	invalid               string
}

func (c WorkspaceOperationsConfig) Resolved() (WorkspaceOperationsConfig, error) {
	if c.invalid != "" {
		return c, fmt.Errorf("invalid workspace operation setting %s", c.invalid)
	}
	if c.Enabled == nil {
		value := true
		c.Enabled = &value
	}
	if c.PrivateRoot == "" {
		c.PrivateRoot = "/tmp/grasp-private-workspace"
	}
	if !filepath.IsAbs(c.PrivateRoot) || filepath.Clean(c.PrivateRoot) != c.PrivateRoot || c.PrivateRoot == "/" || strings.ContainsRune(c.PrivateRoot, 0) {
		return c, fmt.Errorf("workspace_operations.private_root must be a private absolute persistent directory")
	}
	defaults := []struct {
		field *int64
		value int64
	}{{&c.MaxFileBytes, 512 << 20}, {&c.MaxUncompressedBytes, 2 << 30}, {&c.MaxGitBytes, 512 << 20}, {&c.MaxArchiveBytes, 3 << 30}, {&c.MaxManifestBytes, 64 << 20}, {&c.MaxSpoolBytes, 8 << 30}, {&c.MaxEntries, 100000}, {&c.MaxReservedBytes, 32 << 30}, {&c.MaxScopeReservedBytes, 8 << 30}}
	for _, item := range defaults {
		if *item.field == 0 {
			*item.field = item.value
		}
		if *item.field < 1 || *item.field >= math.MaxInt64/2 {
			return c, fmt.Errorf("workspace operation byte/entry limits must be finite positive integers")
		}
	}
	if c.MaxEntries > int64(math.MaxInt) || c.MaxGitBytes >= c.MaxUncompressedBytes || c.MaxGitBytes > c.MaxFileBytes || c.MaxFileBytes > c.MaxUncompressedBytes-c.MaxGitBytes || c.MaxScopeReservedBytes > c.MaxReservedBytes || c.MaxArchiveBytes > c.MaxScopeReservedBytes-c.MaxManifestBytes || c.MaxArchiveBytes > c.MaxSpoolBytes-c.MaxManifestBytes {
		return c, fmt.Errorf("workspace operation limits must fit combined expanded, spool and reservation budgets")
	}
	if c.MaxDuration == "" {
		c.MaxDuration = "30m"
	}
	duration, err := time.ParseDuration(c.MaxDuration)
	if err != nil || duration <= 0 {
		return c, fmt.Errorf("workspace_operations.max_duration must be a finite positive duration")
	}
	if c.MaxWorkers == 0 {
		c.MaxWorkers = 2
	}
	if c.MaxWorkers < 1 || c.MaxWorkers > 64 {
		return c, fmt.Errorf("workspace_operations.max_workers must be between 1 and 64")
	}
	return c, nil
}

func applyWorkspaceOperationsEnv(c *Config) {
	value := &c.WorkspaceOperations
	if raw := env("GRASP_WORKSPACE_OPERATIONS_ENABLED"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			value.invalid = "GRASP_WORKSPACE_OPERATIONS_ENABLED"
		} else {
			value.Enabled = &enabled
		}
	}
	if raw := env("GRASP_WORKSPACE_PRIVATE_ROOT"); raw != "" {
		value.PrivateRoot = raw
	}
	if raw := env("GRASP_WORKSPACE_MAX_DURATION"); raw != "" {
		value.MaxDuration = raw
	}
	for name, field := range map[string]*int64{"MAX_FILE_BYTES": &value.MaxFileBytes, "MAX_UNCOMPRESSED_BYTES": &value.MaxUncompressedBytes, "MAX_GIT_BYTES": &value.MaxGitBytes, "MAX_ARCHIVE_BYTES": &value.MaxArchiveBytes, "MAX_MANIFEST_BYTES": &value.MaxManifestBytes, "MAX_SPOOL_BYTES": &value.MaxSpoolBytes, "MAX_ENTRIES": &value.MaxEntries, "MAX_RESERVED_BYTES": &value.MaxReservedBytes, "MAX_SCOPE_RESERVED_BYTES": &value.MaxScopeReservedBytes} {
		if raw := env("GRASP_WORKSPACE_" + name); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed <= 0 {
				value.invalid = "GRASP_WORKSPACE_" + name
			} else {
				*field = parsed
			}
		}
	}
	if raw := env("GRASP_WORKSPACE_MAX_WORKERS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			value.invalid = "GRASP_WORKSPACE_MAX_WORKERS"
		} else {
			value.MaxWorkers = parsed
		}
	}
}
