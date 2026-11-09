// Package workspacejob runs explicitly registered workspace operations under a
// persisted identity and renewable lease. A completed operation is not proof of
// a snapshot: capture/restore implementations must register their own executor.
package workspacejob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

const Version = 1

var (
	ErrInvalid     = errors.New("invalid workspace job")
	ErrUnsupported = errors.New("workspace operation is not installed")
	ErrFence       = errors.New("workspace operation ownership lost or expired")
	ErrLineage     = errors.New("workspace execution lineage does not match retained runtime")
	ErrConflict    = errors.New("workspace operation identity conflict")
	ErrUnknown     = errors.New("workspace operation needs explicit reconciliation")
	ErrCancelled   = errors.New("workspace operation cancelled")
	ErrClosed      = errors.New("workspace operation service closed")
	ErrCorrupt     = errors.New("workspace operation journal is corrupt")
	ErrInUse       = errors.New("workspace operation journal is already open")
	ErrLimit       = errors.New("workspace operation exceeds its limit")
	idPattern      = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)
	kindPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

type Spec struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Resource string          `json:"resource"`
	Lineage  string          `json:"lineage"`
	Nonce    string          `json:"nonce"`
	Deadline time.Time       `json:"deadline"`
	Body     json.RawMessage `json:"body"`
}
type Fence struct {
	Owner      string    `json:"owner"`
	Generation uint64    `json:"generation"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type CreateRequest struct {
	Spec   Spec   `json:"spec"`
	SHA256 string `json:"sha256"`
	Fence  Fence  `json:"fence"`
}
type RenewRequest struct {
	SHA256  string `json:"sha256"`
	Lineage string `json:"lineage"`
	Fence   Fence  `json:"fence"`
}
type Receipt struct {
	Version    int             `json:"version"`
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Resource   string          `json:"resource"`
	Lineage    string          `json:"lineage"`
	SHA256     string          `json:"sha256"`
	Generation uint64          `json:"generation"`
	ExpiresAt  time.Time       `json:"expiresAt"`
	Deadline   time.Time       `json:"deadline"`
	State      string          `json:"state"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}
type Capabilities struct {
	Version              int      `json:"version"`
	Resource             string   `json:"resource"`
	Lineage              string   `json:"lineage"`
	RenewableLease       bool     `json:"renewableLease"`
	PersistentGeneration bool     `json:"persistentGeneration"`
	Kinds                []string `json:"kinds"`
	MaxLeaseMillis       int64    `json:"maxLeaseMillis"`
	MaxDurationMillis    int64    `json:"maxDurationMillis"`
	// These remain false in the control foundation. Real collectors/restorers
	// must supply separate verified product capabilities, not infer readiness.
	FileCapture bool `json:"fileCapture"`
	FileRestore bool `json:"fileRestore"`
}

type Executor func(context.Context, *Handle, json.RawMessage) (json.RawMessage, error)

type Config struct {
	Root        string
	Resource    string
	Lineage     string
	MaxLease    time.Duration
	MaxDuration time.Duration
	MaxJobs     int
	Executors   map[string]Executor
}

func canonical(body json.RawMessage) (json.RawMessage, error) {
	if len(body) == 0 {
		body = json.RawMessage(`{}`)
	}
	if len(body) > 64<<10 {
		return nil, ErrLimit
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, ErrInvalid
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	normalized, err := json.Marshal(value)
	if len(normalized) > 64<<10 {
		return nil, ErrLimit
	}
	return normalized, err
}

func Fingerprint(spec Spec) (Spec, string, error) {
	if !idPattern.MatchString(spec.ID) || !kindPattern.MatchString(spec.Kind) || !idPattern.MatchString(spec.Resource) || !idPattern.MatchString(spec.Lineage) || !idPattern.MatchString(spec.Nonce) || spec.Deadline.IsZero() {
		return spec, "", ErrInvalid
	}
	body, err := canonical(spec.Body)
	if err != nil {
		return spec, "", err
	}
	spec.Body, spec.Deadline = body, spec.Deadline.UTC()
	data, err := json.Marshal(spec)
	if err != nil {
		return spec, "", err
	}
	hash := sha256.Sum256(data)
	return spec, hex.EncodeToString(hash[:]), nil
}

func ownerHash(owner string) string {
	sum := sha256.Sum256([]byte(owner))
	return hex.EncodeToString(sum[:])
}

func terminal(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled" || state == "unknown"
}
