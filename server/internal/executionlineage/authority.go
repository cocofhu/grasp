// Package executionlineage fences execution against the current SQL dataset
// and records sources that require explicit review after offline restoration.
// It never reads or writes an independent object-store control marker.
package executionlineage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUnavailable   = errors.New("SQL execution binding is unavailable")
	ErrUninitialized = errors.New("execution binding needs explicit offline initialization")
	ErrInactive      = errors.New("execution is inactive during offline recovery")
	ErrConflict      = errors.New("execution binding identity or operation changed")
	ErrUnbound       = errors.New("SQL execution dataset does not match its prepared binding")
	ErrQuarantined   = errors.New("restored execution requires explicit review")
	ErrTransaction   = errors.New("execution operation requires a different SQL transaction boundary")
)

const (
	Initializing   = "initializing"
	Restoring      = "restoring"
	Active         = "active"
	TicketProtocol = "grasp-sql-execution-ticket/v1"
)

// Marker is frozen SQL comparison metadata, not an external capability.
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

// SQLAuthority exposes explicit offline recovery. OpenSQL neither initializes
// schema nor acquires maintenance; ordinary serving uses NewLocalController.
type SQLAuthority struct{ db *gorm.DB }

func OpenSQL(ctx context.Context, db *gorm.DB) (*SQLAuthority, error) {
	if db == nil {
		return nil, ErrUnavailable
	}
	if err := outsideSQLTransaction(db); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &SQLAuthority{db: db}, nil
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
	// A verified backup can predate execution bindings. Only its active SQL
	// binding may record unknown source identity; a restoring ticket must still
	// identify the actual bound installation from which recovery began.
	unboundSource := m.PreviousDatasetID == "" && m.PreviousLineage == "" && validSHA(m.BackupSHA256, false)
	return m.State == Initializing && m.Revision == 1 && initial || m.State == Restoring && m.Revision > 1 && restored || m.State == Active && m.Revision >= 2 && (initial || restored || unboundSource)
}

func markerFromBinding(binding models.ExecutionDataset) (Marker, error) {
	marker := Marker{Version: 1, NamespaceID: binding.NamespaceID, DatasetID: binding.DatasetID, Lineage: binding.Lineage,
		Revision: binding.ControlRevision, State: Active, OperationID: binding.OperationID, BackupSHA256: binding.BackupSHA256,
		PreviousDatasetID: binding.SourceDatasetID, PreviousLineage: binding.SourceLineage, UpdatedAt: binding.BoundAt}
	if binding.ID != "primary" || !marker.valid() {
		return Marker{}, ErrUnbound
	}
	return marker, nil
}

func (a *SQLAuthority) Inspect(ctx context.Context) (Marker, error) {
	if a == nil || a.db == nil {
		return Marker{}, ErrUnavailable
	}
	if err := outsideSQLTransaction(a.db); err != nil {
		return Marker{}, err
	}
	if err := noPendingRecovery(a.db.WithContext(ctx)); err != nil {
		return Marker{}, err
	}
	binding, present, err := observeDataset(ctx, a.db)
	if err != nil {
		return Marker{}, errors.Join(ErrUnavailable, err)
	}
	if !present {
		return Marker{}, ErrUninitialized
	}
	return markerFromBinding(binding)
}

type permitData struct {
	authority  *SQLAuthority
	controller *Controller
	marker     Marker
}

// Permit is private. Its validity comes from current SQL comparisons, not a
// wall-clock TTL; legitimate object staging may take arbitrarily longer.
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
func (a *SQLAuthority) ReadActive(ctx context.Context) (Permit, error) {
	marker, err := a.Inspect(ctx)
	if err != nil {
		return Permit{}, err
	}
	return a.permit(marker), nil
}
func (a *SQLAuthority) permit(marker Marker) Permit {
	return Permit{data: &permitData{authority: a, marker: marker}}
}
func (a *SQLAuthority) Current(ctx context.Context) (string, error) {
	permit, err := a.ReadActive(ctx)
	return permit.Lineage(), err
}
func (a *SQLAuthority) Verify(ctx context.Context, lineage string) error {
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
	authority *SQLAuthority
	marker    Marker
}
type Ticket struct{ data *ticketData }

// TicketDocument is persisted in the locked offline workdir. It has no online
// authority. Old v1 external-marker tickets are never silently converted.
type TicketDocument struct {
	Version  int    `json:"version"`
	Protocol string `json:"protocol"`
	Marker   Marker `json:"marker"`
}

func (t Ticket) Document() TicketDocument {
	if t.data == nil {
		return TicketDocument{}
	}
	return TicketDocument{Version: 2, Protocol: TicketProtocol, Marker: t.data.marker}
}
func ticketFor(a *SQLAuthority, marker Marker) Ticket {
	return Ticket{data: &ticketData{authority: a, marker: marker}}
}
func documentSHA(document TicketDocument) string {
	body, _ := json.Marshal(document)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func validDocument(document TicketDocument) bool {
	return document.Version == 2 && document.Protocol == TicketProtocol && document.Marker.valid() &&
		(document.Marker.State == Initializing || document.Marker.State == Restoring)
}

// The control row serializes with runtime registration and online guards.
// There is no runtime-presence TTL or implicit stale-process deletion.
func offlineGuardTx(tx *gorm.DB, owner string) error {
	if tx == nil || !validUUID(owner) {
		return ErrConflict
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrTransaction
	}
	var control storageguard.Control
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("name = ? AND owner = ?", "primary", owner).Take(&control).Error; err != nil {
		return errors.Join(ErrInactive, err)
	}
	var active int64
	if err := tx.Model(&storageguard.Runtime{}).Count(&active).Error; err != nil {
		return err
	}
	if active != 0 {
		return ErrInactive
	}
	return nil
}
func (a *SQLAuthority) offline(ctx context.Context, owner string, body func(*gorm.DB) error) error {
	if a == nil || a.db == nil {
		return ErrUnavailable
	}
	if err := outsideSQLTransaction(a.db); err != nil {
		return err
	}
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := offlineGuardTx(tx, owner); err != nil {
			return err
		}
		return body(tx)
	})
}

func noPendingRecovery(db *gorm.DB) error {
	return pendingRecoveryCheck(db, true)
}

func pendingRecoveryCheck(db *gorm.DB, lock bool) error {
	// This single newer metadata table can be absent in a pre-change backup.
	// Preserve metadata failures instead of treating HasTable=false as proof.
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if !containsTable(tables, "execution_recoveries") {
		return nil
	}
	return pendingRecoveryRows(db, lock)
}

func strictNoPendingRecovery(db *gorm.DB) error {
	return pendingRecoveryRows(db, true)
}

func pendingRecoveryRows(db *gorm.DB, lock bool) error {
	query := db.Model(&models.ExecutionRecovery{}).Select("id").Where("COALESCE(state, '') <> 'finalized' OR finalized_at IS NULL").Limit(1)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "SHARE"})
	}
	var rows []struct{ ID string }
	err := query.Find(&rows).Error
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if len(rows) != 0 {
		return ErrInactive
	}
	return nil
}
func recoveryMatches(row models.ExecutionRecovery, document TicketDocument) bool {
	raw, _ := json.Marshal(document.Marker)
	return row.ID == document.Marker.OperationID && row.Protocol == TicketProtocol && row.MarkerJSON == string(raw) && row.TicketSHA256 == documentSHA(document) &&
		(row.State == document.Marker.State && row.FinalizedAt == nil || row.State == "finalized" && row.FinalizedAt != nil)
}
func (a *SQLAuthority) recordTx(tx *gorm.DB, document TicketDocument) error {
	if err := noPendingRecovery(tx); err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&models.ExecutionRecovery{}).Where("id = ?", document.Marker.OperationID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrConflict
	}
	raw, _ := json.Marshal(document.Marker)
	return tx.Create(&models.ExecutionRecovery{ID: document.Marker.OperationID, Protocol: TicketProtocol, MarkerJSON: string(raw), State: document.Marker.State, TicketSHA256: documentSHA(document), CreatedAt: time.Now().UTC()}).Error
}
func (a *SQLAuthority) existingTicketTx(tx *gorm.DB, owner, state, backup string) (Ticket, bool, error) {
	var row models.ExecutionRecovery
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row, "id = ?", owner).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Ticket{}, false, nil
	}
	if err != nil {
		return Ticket{}, false, err
	}
	var marker Marker
	if json.Unmarshal([]byte(row.MarkerJSON), &marker) != nil {
		return Ticket{}, false, ErrConflict
	}
	document := TicketDocument{Version: 2, Protocol: TicketProtocol, Marker: marker}
	if !validDocument(document) || marker.State != state || marker.BackupSHA256 != backup || !recoveryMatches(row, document) {
		return Ticket{}, false, ErrConflict
	}
	return ticketFor(a, marker), true, nil
}
func (a *SQLAuthority) resume(ctx context.Context, document TicketDocument, importedSHA string) (Ticket, error) {
	if !validDocument(document) {
		return Ticket{}, ErrConflict
	}
	if importedSHA != "" && (document.Marker.State != Restoring || importedSHA != document.Marker.BackupSHA256) {
		return Ticket{}, ErrConflict
	}
	err := a.offline(ctx, document.Marker.OperationID, func(tx *gorm.DB) error {
		var row models.ExecutionRecovery
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row, "id = ?", document.Marker.OperationID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) && importedSHA != "" {
			// This narrowly named path is called only after the CLI verifies
			// the original workdir, backup SHA and imported target checkpoint.
			return a.recordTx(tx, document)
		}
		if err != nil {
			return errors.Join(ErrConflict, err)
		}
		if !recoveryMatches(row, document) {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return Ticket{}, err
	}
	return ticketFor(a, document.Marker), nil
}
func (a *SQLAuthority) Resume(ctx context.Context, document TicketDocument) (Ticket, error) {
	return a.resume(ctx, document, "")
}

// ResumeImported re-registers the exact original restore intent after SQL
// import. The offline CLI must first validate its original checkpoint, workdir
// and backup hash; this function neither verifies files nor grants execution.
func (a *SQLAuthority) ResumeImported(ctx context.Context, document TicketDocument, verifiedBackupSHA string) (Ticket, error) {
	if !validSHA(verifiedBackupSHA, false) {
		return Ticket{}, ErrConflict
	}
	return a.resume(ctx, document, verifiedBackupSHA)
}

func (a *SQLAuthority) Initialize(ctx context.Context, operationID string) (Ticket, error) {
	var marker Marker
	err := a.offline(ctx, operationID, func(tx *gorm.DB) error {
		if existing, found, err := a.existingTicketTx(tx, operationID, Initializing, ""); err != nil {
			return err
		} else if found {
			marker = existing.data.marker
			return nil
		}
		var binding models.ExecutionDataset
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&binding, "id = ?", "primary").Error
		if err == nil {
			return ErrConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		marker = Marker{Version: 1, NamespaceID: uuid.NewString(), DatasetID: uuid.NewString(), Lineage: uuid.NewString(), Revision: 1, State: Initializing, OperationID: operationID, UpdatedAt: time.Now().UTC()}
		return a.recordTx(tx, ticketFor(a, marker).Document())
	})
	if err != nil {
		return Ticket{}, err
	}
	return ticketFor(a, marker), nil
}

// BeginRestore requires the already-held SQL maintenance barrier. Persist its
// Document before importing SQL, and resume that same ticket on every retry.
func (a *SQLAuthority) BeginRestore(ctx context.Context, operationID, backupSHA256 string) (Ticket, error) {
	if !validSHA(backupSHA256, false) {
		return Ticket{}, ErrConflict
	}
	var marker Marker
	err := a.offline(ctx, operationID, func(tx *gorm.DB) error {
		if existing, found, err := a.existingTicketTx(tx, operationID, Restoring, backupSHA256); err != nil {
			return err
		} else if found {
			marker = existing.data.marker
			return nil
		}
		var binding models.ExecutionDataset
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&binding, "id = ?", "primary").Error; err != nil {
			return errors.Join(ErrUnbound, err)
		}
		var err error
		marker, err = markerFromBinding(binding)
		if err != nil {
			return err
		}
		if marker.Revision >= math.MaxUint64-1 {
			return ErrConflict
		}
		marker.PreviousDatasetID, marker.PreviousLineage = marker.DatasetID, marker.Lineage
		marker.DatasetID, marker.Lineage = uuid.NewString(), uuid.NewString()
		marker.State, marker.Revision, marker.OperationID, marker.BackupSHA256 = Restoring, marker.Revision+1, operationID, backupSHA256
		marker.UpdatedAt = time.Now().UTC()
		return a.recordTx(tx, ticketFor(a, marker).Document())
	})
	if err != nil {
		return Ticket{}, err
	}
	return ticketFor(a, marker), nil
}
