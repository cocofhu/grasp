package payloadstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ReferenceReport struct {
	Cells       int64  `json:"cells"`
	Payloads    int64  `json:"payloads"`
	Attachments int64  `json:"attachments"`
	SHA256      string `json:"sha256"`
}

// VerifyReferences walks raw SQL rather than model hooks, so a missing object
// cannot disappear behind an empty/default deserialized value. It follows JSON
// payloads, pending-turn bytes and image ref fields recursively. It reads the
// destination alone: a retained local file must not hide an incomplete replica.
// This is a verification boundary, not permission to collect unreferenced data.
func VerifyReferences(ctx context.Context, db *gorm.DB, destination objectstore.Store) (ReferenceReport, error) {
	var report ReferenceReport
	h := sha256.New()
	cellHash := json.NewEncoder(h)
	attachments, err := blob.NewObjects(destination, nil, 0)
	if err != nil {
		return report, err
	}
	seenPayloads, seenBlobs := map[string]bool{}, map[string]bool{}
	for _, model := range models.AllModels() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return report, err
		}
		skip, err := filestore.SkipMissingPayloadTable(ctx, db, stmt.Schema.Table)
		if err != nil {
			return report, err
		}
		if skip {
			continue
		}
		columns := []string{}
		serializers := map[string]string{}
		descriptors := map[string]filestore.PayloadField{}
		fields, err := filestore.PayloadFields(stmt.Schema)
		if err != nil {
			return report, err
		}
		for _, field := range fields {
			// New explicit roots may not exist in a pre-upgrade SQL snapshot.
			// Its verified database bytes cannot reference an absent column.
			if field.Explicit && !db.Migrator().HasColumn(stmt.Schema.Table, field.Column) {
				if filestore.OptionalPayloadTable(stmt.Schema.Table) {
					return report, fmt.Errorf("existing payload table %s is missing column %s", stmt.Schema.Table, field.Column)
				}
				continue
			}
			columns = append(columns, field.Column)
			serializers[field.Column], descriptors[field.Column] = field.Serializer, field
		}
		if len(columns) == 0 {
			continue
		}
		// Keyset paging releases the sole SQLite connection before object GETs.
		// All current payload-bearing tables have one primary key; fail closed
		// when a future model needs a different cursor instead of skipping it.
		if len(stmt.Schema.PrimaryFields) != 1 {
			return report, fmt.Errorf("reference scan requires primary cursor: %s", stmt.Schema.Table)
		}
		pk := stmt.Schema.PrimaryFields[0].DBName
		columns = append(columns, pk)
		selected := make([]string, 0, len(columns))
		for _, column := range columns {
			selected = append(selected, stmt.Quote(column))
		}
		var after any
		for {
			q := db.WithContext(ctx).Table(stmt.Schema.Table).Select(selected).
				Order(clause.OrderByColumn{Column: clause.Column{Name: pk}}).Limit(1)
			if after != nil {
				q = q.Where(clause.Gt{Column: clause.Column{Name: pk}, Value: after})
			}
			var rows []map[string]any
			if err := q.Find(&rows).Error; err != nil {
				return report, err
			}
			if len(rows) == 0 {
				break
			}
			row := rows[0]
			after = row[pk]
			for _, column := range columns[:len(columns)-1] {
				serializer := serializers[column]
				descriptor := descriptors[column]
				raw, err := storedBytes(row[column])
				if err != nil {
					return report, fmt.Errorf("stored payload %s.%s: %w", stmt.Schema.Table, column, err)
				}
				if len(raw) == 0 || !descriptor.Explicit && string(raw) == "null" {
					continue
				}
				if err := cellHash.Encode([]string{stmt.Schema.Table, fmt.Sprint(after), column, string(raw)}); err != nil {
					return report, err
				}
				report.Cells++
				if descriptor.Explicit && !filestore.IsRef(string(raw)) {
					return report, fmt.Errorf("explicit payload requires a prepared reference in %s.%s", stmt.Schema.Table, column)
				}
				if strings.HasPrefix(string(raw), filestore.Prefix) && (strings.HasPrefix(serializer, "file") || descriptor.Explicit) {
					ref := string(raw)
					encoding, digest, err := filestore.Parse(ref)
					if err != nil {
						return report, fmt.Errorf("invalid reference in %s.%s", stmt.Schema.Table, column)
					}
					if descriptor.Explicit && encoding != descriptor.Encoding {
						return report, fmt.Errorf("payload encoding disagrees with %s.%s", stmt.Schema.Table, column)
					}
					if seenPayloads[ref] {
						continue
					}
					key := "payloads/sha256/" + digest[:2] + "/" + digest
					file, err := verifiedPayload(ctx, destination, key, digest)
					if err != nil {
						return report, fmt.Errorf("reference %s.%s: %w", stmt.Schema.Table, column, err)
					}
					seenPayloads[ref], report.Payloads = true, report.Payloads+1
					if encoding == filestore.JSON || encoding == filestore.Bytes {
						raw, err = io.ReadAll(file)
					} else {
						raw = nil
					}
					closeErr := file.Close()
					if err != nil {
						return report, err
					}
					if closeErr != nil {
						return report, closeErr
					}
					if encoding == filestore.JSON && !json.Valid(raw) {
						return report, errors.New("referenced JSON payload is invalid")
					}
				}
				if serializer == "filetext" || len(raw) == 0 {
					continue
				}
				if !json.Valid(raw) {
					if serializer == "filebytes" || descriptor.Explicit && descriptor.Encoding == filestore.Bytes {
						continue
					}
					return report, fmt.Errorf("invalid stored JSON in %s.%s", stmt.Schema.Table, column)
				}
				var value any
				if err := json.Unmarshal(raw, &value); err != nil {
					return report, err
				}
				if err := verifyImageRefs(ctx, value, attachments, seenBlobs, &report); err != nil {
					return report, fmt.Errorf("attachment reference %s.%s: %w", stmt.Schema.Table, column, err)
				}
			}
		}
	}
	report.SHA256 = hex.EncodeToString(h.Sum(nil))
	return report, nil
}

func storedBytes(value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case *any:
		// GORM's map scan retains this wrapper for SQLite LONGTEXT columns.
		// Unwrap the driver value, still rejecting every non-byte SQL type.
		if v == nil {
			return nil, nil
		}
		return storedBytes(*v)
	default:
		return nil, fmt.Errorf("unexpected serialized SQL value (%T)", value)
	}
}

func verifiedPayload(ctx context.Context, store objectstore.Store, key, digest string) (*objectstore.TemporaryFile, error) {
	body, info, err := store.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return objectstore.DownloadVerified(ctx, body, info.Size, digest)
}

func verifyImageRefs(ctx context.Context, value any, attachments blob.Store, seen map[string]bool, report *ReferenceReport) error {
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			if err := verifyImageRefs(ctx, child, attachments, seen, report); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range v {
			if ref, ok := child.(string); ok && key == "ref" && strings.HasPrefix(ref, "blob:") {
				parsed, err := blob.ParseRef(ref)
				if err != nil {
					return errors.New("invalid attachment reference")
				}
				if seen[ref] {
					continue
				}
				body, _, err := attachments.Open(ctx, parsed)
				if err != nil {
					return err
				}
				if err := body.Close(); err != nil {
					return err
				}
				seen[ref], report.Attachments = true, report.Attachments+1
			} else if err := verifyImageRefs(ctx, child, attachments, seen, report); err != nil {
				return err
			}
		}
	}
	return nil
}
