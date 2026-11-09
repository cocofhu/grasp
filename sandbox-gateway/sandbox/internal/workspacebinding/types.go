// Package workspacebinding owns the runtime's durable logical working root.
// A binding changes files and future consumers, never process memory or a
// coding tool's private conversation. Host directories are startup-owned.
package workspacebinding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"backend/internal/workspacejob"
)

const Version = 1

var (
	ErrInvalid     = errors.New("invalid workspace binding")
	ErrConflict    = errors.New("workspace binding changed")
	ErrBusy        = errors.New("workspace consumers are busy")
	ErrUnknown     = errors.New("workspace switch requires its original receipt")
	ErrUninstalled = errors.New("workspace binding consumers are not installed")
	ErrCorrupt     = errors.New("workspace binding journal is corrupt")
	ErrClosed      = errors.New("workspace binding manager is closed")
	idPattern      = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)
	hashPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Reference is logical identity. It deliberately contains no physical lease
// generation: a lease renewal cannot change a user's working directory.
type Reference struct {
	ID          string `json:"id"`
	Revision    uint64 `json:"revision"`
	Fingerprint string `json:"fingerprint"`
}

type Selector struct {
	Kind string `json:"kind"` // original, restore, or a previously retained binding
	ID   string `json:"id,omitempty"`
}

type Head struct {
	Version   int       `json:"version"`
	Resource  string    `json:"resource"`
	Lineage   string    `json:"lineage"`
	Reference Reference `json:"reference"`
	Target    Selector  `json:"target"`
	ParentID  string    `json:"parentId,omitempty"`
}

type Spec struct {
	Version   int       `json:"version"`
	Expected  Reference `json:"expected"`
	BindingID string    `json:"bindingId"`
	Target    Selector  `json:"target"`
	// ReturnSnapshotID is an independently ready snapshot proved by the
	// authenticated control plane. Its bytes are not a substitute for the old
	// root, which is retained on every switch.
	ReturnSnapshotID string `json:"returnSnapshotId"`
	Authorization    string `json:"authorization"`
}

type Result struct {
	Version          int      `json:"version"`
	OperationID      string   `json:"operationId"`
	Previous         Head     `json:"previous"`
	Active           Head     `json:"active"`
	ReturnSnapshotID string   `json:"returnSnapshotId"`
	Consumers        []string `json:"consumers"`
	ProcessMemory    bool     `json:"processMemory"`
}

// Operation is separate from the generic native job journal. A head committed
// just before a runtime crash remains provable even if that job is unknown.
// Reading this receipt never repeats a switch or rewrites the native journal.
type Operation struct {
	Version     int                   `json:"version"`
	Identity    workspacejob.Identity `json:"identity"`
	State       string                `json:"state"`
	Result      *Result               `json:"result,omitempty"`
	CreatedAt   time.Time             `json:"createdAt"`
	CompletedAt *time.Time            `json:"completedAt,omitempty"`
}

type View struct {
	Head Head `json:"head"`
	// Path is private authenticated data-plane metadata. Public product DTOs
	// must expose Reference only; a caller can never submit Path as a target.
	Path               string `json:"path"`
	PendingOperationID string `json:"pendingOperationId,omitempty"`
	Ready              bool   `json:"ready"`
}

type Capabilities struct {
	Version       int      `json:"version"`
	Resource      string   `json:"resource"`
	Lineage       string   `json:"lineage"`
	Activate      bool     `json:"activate"`
	Return        bool     `json:"return"`
	DurableHead   bool     `json:"durableHead"`
	Consumers     []string `json:"consumers"`
	ProcessMemory bool     `json:"processMemory"`
	PortOwnership bool     `json:"portOwnership"`
}

type PortView struct {
	Version   int       `json:"version"`
	Reference Reference `json:"reference"`
	Port      int       `json:"port"`
	Verified  bool      `json:"verified"`
}

// Consumer must prepare without mutating its live root. Apply runs after the
// fsynced intent and Verify must prove the old consumer cannot still accept
// work. Failure after intent leaves an explicit unknown barrier, never a fake
// rollback. Close releases local admission locks only.
type Consumer interface {
	Name() string
	Prepare(context.Context, string, string) (Transition, error)
}
type Transition interface {
	Apply(context.Context) error
	Verify(context.Context) error
	Close() error
}

type Config struct {
	ControlRoot   string
	OriginalRoot  string
	RestoreRoot   string
	Resource      string
	Lineage       string
	MaxOperations int
	// ResolveRestore accepts only a completed native materialization receipt
	// belonging to this same runtime's trusted job service.
	ResolveRestore func(context.Context, string) error
}

func validReference(ref Reference) bool {
	return idPattern.MatchString(ref.ID) && ref.Revision > 0 && hashPattern.MatchString(ref.Fingerprint)
}

func ValidReference(ref Reference) bool { return validReference(ref) }

func fingerprint(head Head) string {
	head.Reference.Fingerprint = ""
	data, _ := json.Marshal(head)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func initialHead(resource, lineage string) Head {
	sum := sha256.Sum256([]byte(resource + "\x00" + lineage + "\x00original"))
	head := Head{Version: Version, Resource: resource, Lineage: lineage, Reference: Reference{ID: hex.EncodeToString(sum[:]), Revision: 1}, Target: Selector{Kind: "original"}}
	head.Reference.Fingerprint = fingerprint(head)
	return head
}
