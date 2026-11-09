// Package workspacesnapshot captures selected authorized working directories
// and materializes their Git state without changing or committing the source.
// It preserves file bytes, not process memory or native CLI private sessions.
package workspacesnapshot

import (
	"errors"
	"regexp"
	"time"
)

const (
	Version       = 1
	PolicyVersion = "workspace-files-v1"
	ArchiveName   = "workspace.tar.gz"
	ManifestName  = "manifest.json"
)

var (
	ErrUnsafe         = errors.New("unsafe workspace snapshot path or metadata")
	ErrLimit          = errors.New("workspace snapshot quota exceeded")
	ErrChanged        = errors.New("workspace source changed during capture")
	ErrGitUnsupported = errors.New("workspace Git metadata cannot be safely materialized")
	ErrIntegrity      = errors.New("workspace snapshot differs from its verified manifest")
	oidPattern        = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	hashPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Limits are frozen in the admitted capture policy. They are independent of
// the smaller immutable-commit import quota and apply before publication.
type Limits struct {
	MaxFileBytes     int64 `json:"maxFileBytes"`
	MaxBytes         int64 `json:"maxBytes"`
	MaxGitBytes      int64 `json:"maxGitBytes"`
	MaxArchiveBytes  int64 `json:"maxArchiveBytes"`
	MaxManifestBytes int64 `json:"maxManifestBytes"`
	MaxSpoolBytes    int64 `json:"maxSpoolBytes"`
	MaxEntries       int   `json:"maxEntries"`
	MaxRepositories  int   `json:"maxRepositories"`
	MaxScanEntries   int   `json:"maxScanEntries"`
}

func DefaultLimits() Limits {
	return Limits{MaxFileBytes: 512 << 20, MaxBytes: 2 << 30, MaxGitBytes: 2 << 30, MaxArchiveBytes: 3 << 30, MaxManifestBytes: 64 << 20, MaxSpoolBytes: 8 << 30, MaxEntries: 100000, MaxRepositories: 64, MaxScanEntries: 500000}
}

type Spec struct {
	PolicyVersion  string   `json:"policyVersion"`
	PolicySHA256   string   `json:"policySha256"`
	Limits         Limits   `json:"limits"`
	Paths          []string `json:"paths"`
	IncludeIgnored []string `json:"includeIgnored,omitempty"`
}

type Entry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   int64  `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}
type ArchiveManifest struct {
	Version int     `json:"version"`
	Bytes   int64   `json:"bytes"`
	Entries []Entry `json:"entries"`
}
type Exclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type GitRef struct {
	Name string `json:"name"`
	OID  string `json:"oid"`
}
type IndexEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	OID    string `json:"oid"`
	Stage  int    `json:"stage"`
	Assume bool   `json:"assumeUnchanged,omitempty"`
	Skip   bool   `json:"skipWorktree,omitempty"`
	Intent bool   `json:"intentToAdd,omitempty"`
}
type Repository struct {
	Path         string         `json:"path"`
	ObjectFormat string         `json:"objectFormat"`
	HEAD         string         `json:"head,omitempty"`
	SymbolicHEAD string         `json:"symbolicHead,omitempty"`
	Refs         []GitRef       `json:"refs"`
	Index        []IndexEntry   `json:"index"`
	Pack         string         `json:"pack,omitempty"`
	PackSHA256   string         `json:"packSha256,omitempty"`
	PackBytes    int64          `json:"packBytes,omitempty"`
	Linked       bool           `json:"linked"`
	Operation    []GitOperation `json:"operation,omitempty"`
}

type GitOperation struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}
type Manifest struct {
	Version        int             `json:"version"`
	PolicyVersion  string          `json:"policyVersion"`
	Consistency    string          `json:"consistency"`
	Paths          []string        `json:"paths"`
	IncludeIgnored []string        `json:"includeIgnored,omitempty"`
	Limits         Limits          `json:"limits"`
	PolicySHA256   string          `json:"policySha256"`
	Files          []Entry         `json:"files"`
	Repositories   []Repository    `json:"repositories"`
	Excluded       []Exclusion     `json:"excluded"`
	Archive        ArchiveManifest `json:"archive"`
	CreatedAt      time.Time       `json:"createdAt"`
}
type Artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type Receipt struct {
	Version     int      `json:"version"`
	Archive     Artifact `json:"archive"`
	Manifest    Artifact `json:"manifest"`
	Files       int      `json:"files"`
	Bytes       int64    `json:"bytes"`
	Consistency string   `json:"consistency"`
}

type Config struct {
	// Root is a server-authorized directory, supplied by runtime startup. It
	// is never accepted in a browser payload. Linked common Git dirs must
	// also remain underneath this same root.
	Root string
	// PrivatePaths are additional platform-managed relative authorization /
	// runtime-state paths. They augment the mandatory built-in exclusions.
	PrivatePaths []string
	Limits       Limits
	GitBinary    string
}
