// Package executionlineage keeps execution authority outside SQL/payload
// backups. An independent conditional-write namespace is required. Starting a
// server never initializes or activates a missing/corrupt control marker.
package executionlineage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"time"

	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/google/uuid"
)

var (
	ErrUnavailable   = errors.New("independent execution authority is unavailable")
	ErrUninitialized = errors.New("execution authority needs explicit offline initialization")
	ErrInactive      = errors.New("execution authority is inactive during offline recovery")
	ErrConflict      = errors.New("execution authority identity or operation changed")
	ErrUnbound       = errors.New("SQL execution dataset does not match independent authority")
	ErrQuarantined   = errors.New("restored execution requires explicit review")
	ErrTransaction   = errors.New("external authority I/O requires an outer transaction-free boundary")
)

const (
	markerKey    = "execution-namespace.json"
	Initializing = "initializing"
	Restoring    = "restoring"
	Active       = "active"
	permitTTL    = 5 * time.Second
)

type Marker struct {
	Version           int       `json:"version"`
	NamespaceID       string    `json:"namespaceId"`
	DatasetID         string    `json:"datasetId"`
	Lineage           string    `json:"lineage"`
	Revision          uint64    `json:"revision"`
	State             string    `json:"state"`
	OperationID       string    `json:"operationId"`
	BackupSHA256      string    `json:"backupSha256"`
	PreviousDatasetID string    `json:"previousDatasetId"`
	PreviousLineage   string    `json:"previousLineage"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type Authority struct{ store objectstore.ConditionalStore }

// Open tests real conditional writes with its own isolated probe. It does not
// create an execution marker, bind SQL, or grant execution to existing history.
func Open(ctx context.Context, store objectstore.ConditionalStore) (*Authority, error) {
	if err := objectstore.ProbeConditional(ctx, store); err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	return &Authority{store: store}, nil
}

func validUUID(value string) bool { _, err := uuid.Parse(value); return err == nil }

func validSHA(value string, empty bool) bool {
	if value == "" {
		return empty
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func (m Marker) valid() bool {
	if m.Version != 1 || !validUUID(m.NamespaceID) || !validUUID(m.DatasetID) || !validUUID(m.Lineage) || !validUUID(m.OperationID) || m.Revision == 0 || m.Revision == math.MaxUint64 || m.UpdatedAt.IsZero() {
		return false
	}
	initial := m.PreviousDatasetID == "" && m.PreviousLineage == "" && m.BackupSHA256 == ""
	restored := validUUID(m.PreviousDatasetID) && validUUID(m.PreviousLineage) && m.PreviousDatasetID != m.DatasetID && m.PreviousLineage != m.Lineage && validSHA(m.BackupSHA256, false)
	return m.State == Initializing && m.Revision == 1 && initial || m.State == Restoring && m.Revision > 1 && restored || m.State == Active && m.Revision >= 2 && (initial || restored)
}

func markerInfo(body []byte) objectstore.Info {
	sum := sha256.Sum256(body)
	return objectstore.Info{Size: int64(len(body)), ContentType: "application/json", Metadata: map[string]string{"grasp-sha256": hex.EncodeToString(sum[:])}}
}

func (a *Authority) read(ctx context.Context) (Marker, objectstore.Info, error) {
	var marker Marker
	if a == nil || a.store == nil {
		return marker, objectstore.Info{}, ErrUnavailable
	}
	body, info, err := a.store.Open(ctx, markerKey)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return marker, info, ErrUninitialized
		}
		return marker, info, errors.Join(ErrUnavailable, err)
	}
	defer func() { _ = body.Close() }()
	if info.Size < 1 || info.Size > objectstore.MaxConditionalBytes || info.ETag == "" {
		return marker, info, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(body, objectstore.MaxConditionalBytes+1))
	if err != nil || int64(len(raw)) != info.Size || json.Unmarshal(raw, &marker) != nil || !marker.valid() {
		return Marker{}, info, errors.Join(ErrUnavailable, err)
	}
	canonical, _ := json.Marshal(marker)
	sum := sha256.Sum256(raw)
	if !bytes.Equal(raw, canonical) || info.Metadata["grasp-sha256"] != hex.EncodeToString(sum[:]) {
		return Marker{}, info, ErrUnavailable
	}
	return marker, info, ctx.Err()
}

func (a *Authority) Inspect(ctx context.Context) (Marker, error) {
	marker, _, err := a.read(ctx)
	return marker, err
}

type permitData struct {
	authority *Authority
	marker    Marker
	expiresAt time.Time
}

// Permit is a short-lived, private proof of an actual external active read.
// GuardTx performs metadata checks only; long-lived workers must refresh this
// outside their next SQL transaction rather than caching it for their lifetime.
type Permit struct{ data *permitData }

func (p Permit) DatasetID() string {
	if p.data == nil {
		return ""
	}
	return p.data.marker.DatasetID
}

func (p Permit) Lineage() string {
	if p.data == nil {
		return ""
	}
	return p.data.marker.Lineage
}

func (p Permit) NamespaceID() string {
	if p.data == nil {
		return ""
	}
	return p.data.marker.NamespaceID
}

func (a *Authority) ReadActive(ctx context.Context) (Permit, error) {
	marker, _, err := a.read(ctx)
	if err != nil {
		return Permit{}, err
	}
	if marker.State != Active {
		return Permit{}, ErrInactive
	}
	return a.permit(marker), nil
}

func (a *Authority) permit(marker Marker) Permit {
	return Permit{data: &permitData{authority: a, marker: marker, expiresAt: time.Now().Add(permitTTL)}}
}

func (a *Authority) Current(ctx context.Context) (string, error) {
	permit, err := a.ReadActive(ctx)
	return permit.Lineage(), err
}

func (a *Authority) Verify(ctx context.Context, lineage string) error {
	current, err := a.Current(ctx)
	if err != nil {
		return err
	}
	if current != lineage {
		return ErrConflict
	}
	return nil
}

type ticketData struct {
	authority *Authority
	marker    Marker
}

type Ticket struct{ data *ticketData }

// TicketDocument can be persisted in the operator's private workdir. It cannot
// authorize anything until Resume GETs and matches the actual external marker.
type TicketDocument struct {
	Version int    `json:"version"`
	Marker  Marker `json:"marker"`
}

func (t Ticket) Document() TicketDocument {
	if t.data == nil {
		return TicketDocument{}
	}
	return TicketDocument{Version: 1, Marker: t.data.marker}
}

func ticketFor(a *Authority, marker Marker) Ticket {
	return Ticket{data: &ticketData{authority: a, marker: marker}}
}

func sameOperation(a, b Marker) bool {
	return a.Version == b.Version && a.NamespaceID == b.NamespaceID && a.DatasetID == b.DatasetID && a.Lineage == b.Lineage && a.OperationID == b.OperationID && a.BackupSHA256 == b.BackupSHA256 && a.PreviousDatasetID == b.PreviousDatasetID && a.PreviousLineage == b.PreviousLineage
}

func activeChild(current, target Marker) bool {
	return current.State == Active && current.Revision == target.Revision+1 && sameOperation(current, target)
}

// Resume mints a process-local ticket only after an exact external GET. It also
// recognizes the one already-activated child for idempotent finalize retries.
func (a *Authority) Resume(ctx context.Context, document TicketDocument) (Ticket, error) {
	target := document.Marker
	if document.Version != 1 || !target.valid() || target.State != Initializing && target.State != Restoring {
		return Ticket{}, ErrConflict
	}
	current, _, err := a.read(ctx)
	if err != nil {
		return Ticket{}, err
	}
	if current != target && !activeChild(current, target) {
		return Ticket{}, ErrConflict
	}
	return ticketFor(a, target), nil
}

// Initialize is an explicit offline operation. It first creates only an
// inactive marker; SQL binding/quarantine and Activate are still required.
func (a *Authority) Initialize(ctx context.Context, operationID string) (Ticket, error) {
	if !validUUID(operationID) {
		return Ticket{}, ErrConflict
	}
	current, _, err := a.read(ctx)
	if err == nil {
		if current.State == Initializing && current.OperationID == operationID {
			return ticketFor(a, current), nil
		}
		return Ticket{}, ErrConflict
	}
	if !errors.Is(err, ErrUninitialized) {
		return Ticket{}, err
	}
	marker := Marker{Version: 1, NamespaceID: uuid.NewString(), DatasetID: uuid.NewString(), Lineage: uuid.NewString(), Revision: 1, State: Initializing, OperationID: operationID, UpdatedAt: time.Now().UTC()}
	if err := a.publish(ctx, marker, ""); err != nil {
		return Ticket{}, err
	}
	return ticketFor(a, marker), nil
}

// BeginRestore MUST run before importing SQL. It immediately revokes the old
// active lineage in the independent namespace and never imports/replays work.
func (a *Authority) BeginRestore(ctx context.Context, operationID, backupSHA256 string) (Ticket, error) {
	if !validUUID(operationID) || !validSHA(backupSHA256, false) {
		return Ticket{}, ErrConflict
	}
	marker, info, err := a.read(ctx)
	if err != nil {
		return Ticket{}, err
	}
	if marker.State == Restoring && marker.OperationID == operationID && marker.BackupSHA256 == backupSHA256 {
		return ticketFor(a, marker), nil
	}
	if marker.State != Active || marker.Revision >= math.MaxUint64-1 {
		return Ticket{}, ErrInactive
	}
	marker.PreviousDatasetID, marker.PreviousLineage = marker.DatasetID, marker.Lineage
	marker.DatasetID, marker.Lineage = uuid.NewString(), uuid.NewString()
	marker.State, marker.Revision, marker.OperationID, marker.BackupSHA256 = Restoring, marker.Revision+1, operationID, backupSHA256
	marker.UpdatedAt = time.Now().UTC()
	if err := a.publish(ctx, marker, info.ETag); err != nil {
		return Ticket{}, err
	}
	return ticketFor(a, marker), nil
}

func (a *Authority) publish(ctx context.Context, marker Marker, expected string) error {
	body, err := json.Marshal(marker)
	if err != nil || !marker.valid() {
		return ErrConflict
	}
	if expected == "" {
		_, err = a.store.PutIfAbsent(ctx, markerKey, bytes.NewReader(body), markerInfo(body))
	} else {
		_, err = a.store.CompareAndSwap(ctx, markerKey, expected, bytes.NewReader(body), markerInfo(body))
	}
	// Read-back establishes the external inactive barrier before SQL import.
	// A lost write acknowledgement can also be reconciled by this exact body;
	// never mint a new lineage or retry blindly into another generation.
	actual, _, readErr := a.read(ctx)
	if readErr != nil || actual != marker {
		return errors.Join(ErrConflict, err, readErr)
	}
	return nil
}
