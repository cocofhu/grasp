// Package workspacefiles validates the versioned file-capture wire format.
// Runtime execution and storage ownership are supplied by their own services;
// a parsed manifest or a caller's hash is never a publication proof.
package workspacefiles

import (
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacearchive"
)

const PolicyVersion = "workspace-files-v1"

type Spec struct {
	Binding        *sandbox.WorkspaceBindingReference `json:"binding,omitempty"`
	PolicyVersion  string                             `json:"policyVersion"`
	PolicySHA256   string                             `json:"policySha256"`
	Limits         sandbox.WorkspaceFileLimits        `json:"limits"`
	Paths          []string                           `json:"paths"`
	IncludeIgnored []string                           `json:"includeIgnored,omitempty"`
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
type ReadSpec struct {
	SourceJobID  string  `json:"sourceJobId"`
	SourceSHA256 string  `json:"sourceSha256"`
	Receipt      Receipt `json:"receipt"`
}

// GitRef carries exactly one of OID (a direct ref) or Target (a symbolic ref
// such as refs/remotes/origin/HEAD, which every ordinary clone creates).
type GitRef struct {
	Name   string `json:"name"`
	OID    string `json:"oid,omitempty"`
	Target string `json:"target,omitempty"`
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
type GitOperation struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
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
type Exclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type Manifest struct {
	Binding        *sandbox.WorkspaceBindingReference `json:"binding,omitempty"`
	Version        int                                `json:"version"`
	PolicyVersion  string                             `json:"policyVersion"`
	Consistency    string                             `json:"consistency"`
	Paths          []string                           `json:"paths"`
	IncludeIgnored []string                           `json:"includeIgnored,omitempty"`
	Limits         sandbox.WorkspaceFileLimits        `json:"limits"`
	PolicySHA256   string                             `json:"policySha256"`
	Files          []workspacearchive.Entry           `json:"files"`
	Repositories   []Repository                       `json:"repositories"`
	Excluded       []Exclusion                        `json:"excluded"`
	Archive        workspacearchive.Manifest          `json:"archive"`
	CreatedAt      time.Time                          `json:"createdAt"`
}
