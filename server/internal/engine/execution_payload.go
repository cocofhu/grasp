package engine

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// executionRow freezes logical model fields outside SQL. Publishing the raw
// columns below never invokes payload serializers under the lineage lock.
// This is intentionally private to the engine's execution records.
type executionRow struct {
	table  string
	values map[string]any
	proofs []filestore.Prepared
	files  *filestore.Store
	model  reflect.Value
	key    *schema.Field
	keyVal any
	newRow bool
}

func (e *Engine) prepareExecutionRow(ctx context.Context, row any, create bool, columns, omit []string) (*executionRow, error) {
	if _, inside := e.db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return nil, executionlineage.ErrTransaction
	}
	statement := &gorm.Statement{DB: e.db}
	if err := statement.Parse(row); err != nil {
		return nil, err
	}
	value := reflect.ValueOf(row)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return nil, errors.New("execution publication requires one concrete model")
	}
	result := &executionRow{table: statement.Schema.Table, values: map[string]any{}, model: value}
	if len(statement.Schema.PrimaryFields) != 1 {
		return nil, errors.New("execution publication requires one primary key")
	}
	result.key = statement.Schema.PrimaryFields[0]
	primary := result.key.ReflectValueOf(ctx, value)
	result.keyVal = primary.Interface()
	result.newRow = create || primary.IsZero()
	wanted := func(field *schema.Field) bool {
		for _, name := range omit {
			if name == field.Name || name == field.DBName {
				return false
			}
		}
		if len(columns) == 0 {
			return true
		}
		for _, name := range columns {
			if name == field.Name || name == field.DBName {
				return true
			}
		}
		return false
	}
	now := time.Now()
	for _, field := range statement.Schema.Fields {
		if field.DBName == "" || !wanted(field) || (!result.newRow && (!field.Updatable || field.PrimaryKey || field.AutoCreateTime > 0)) ||
			(result.newRow && (!field.Creatable || field.AutoIncrement && field.ReflectValueOf(ctx, value).IsZero())) {
			continue
		}
		logical := field.ReflectValueOf(ctx, value).Interface()
		if result.newRow && field.HasDefaultValue && field.DefaultValueInterface != nil && field.ReflectValueOf(ctx, value).IsZero() {
			logical = field.DefaultValueInterface
			if err := field.Set(ctx, value, logical); err != nil {
				return nil, err
			}
		}
		if field.AutoUpdateTime > 0 || result.newRow && field.AutoCreateTime > 0 && field.ReflectValueOf(ctx, value).IsZero() {
			if field.FieldType != reflect.TypeOf(now) {
				return nil, fmt.Errorf("unsupported execution timestamp %s", field.Name)
			}
			logical = now
			if err := field.Set(ctx, value, now); err != nil {
				return nil, err
			}
		}
		serializer := field.TagSettings["SERIALIZER"]
		switch serializer {
		case "filejson", "filetext", "filebytes":
			encoding := filestore.JSON
			var body []byte
			var err error
			if serializer == "filejson" {
				body, err = json.Marshal(logical)
				if err != nil {
					return nil, err
				}
				if string(body) == "null" {
					result.values[field.DBName] = nil
					continue
				}
			} else {
				encoding = filestore.Text
				if serializer == "filebytes" {
					encoding = filestore.Bytes
				}
				switch text := logical.(type) {
				case string:
					body = []byte(text)
				case []byte:
					body = text
				default:
					return nil, fmt.Errorf("unsupported execution payload %s", field.Name)
				}
				if len(body) == 0 {
					result.values[field.DBName] = ""
					continue
				}
			}
			if result.files == nil {
				result.files, err = filestore.FromDB(e.db)
				if err != nil {
					return nil, err
				}
			}
			prepared, err := result.files.Prepare(ctx, bytes.NewReader(body), encoding)
			if err != nil {
				return nil, err
			}
			result.proofs = append(result.proofs, prepared)
			result.values[field.DBName] = prepared.Ref()
		case "json":
			body, err := json.Marshal(logical)
			if err != nil {
				return nil, err
			}
			if string(body) == "null" {
				result.values[field.DBName] = nil
			} else {
				result.values[field.DBName] = string(body)
			}
		case "":
			result.values[field.DBName] = logical
		default:
			return nil, fmt.Errorf("unsupported execution serializer %s", serializer)
		}
	}
	return result, nil
}

func (r *executionRow) write(tx *gorm.DB, where func(*gorm.DB) *gorm.DB) *gorm.DB {
	for _, proof := range r.proofs {
		if _, err := r.files.PreparedRef(proof); err != nil {
			result := tx.Session(&gorm.Session{})
			_ = result.AddError(err)
			return result
		}
	}
	query := tx.Table(r.table)
	if r.newRow {
		result := query.Create(&r.values)
		if result.Error == nil && r.key.AutoIncrement {
			id, ok := r.values[r.key.DBName]
			if !ok {
				id, ok = r.values["@id"] // raw map has no serializer-bearing schema
			}
			if ok {
				if err := r.key.Set(tx.Statement.Context, r.model, id); err != nil {
					_ = result.AddError(err)
				}
			}
		}
		return result
	}
	if where != nil {
		query = where(query)
	} else {
		query = query.Where(r.key.DBName+" = ?", r.keyVal)
	}
	return query.Updates(r.values)
}

func (e *Engine) executionSave(ctx context.Context, runID string, row any, omit ...string) *gorm.DB {
	if e.execution == nil {
		return e.db.WithContext(ctx).Omit(omit...).Save(row)
	}
	return e.executionPublish(ctx, runID, row, false, nil, omit, nil)
}

func (e *Engine) executionCreate(ctx context.Context, runID string, row any) *gorm.DB {
	if e.execution == nil {
		return e.db.WithContext(ctx).Create(row)
	}
	return e.executionPublish(ctx, runID, row, true, nil, nil, nil)
}

func (e *Engine) executionPublish(ctx context.Context, runID string, row any, create bool, columns, omit []string, where func(*gorm.DB) *gorm.DB) *gorm.DB {
	prepared, err := e.prepareExecutionRow(ctx, row, create, columns, omit)
	var result *gorm.DB
	if err == nil {
		extra := []executionlineage.Entity{}
		if prepared.table == "state_runs" && !prepared.newRow {
			extra = append(extra, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: fmt.Sprint(prepared.keyVal)})
		}
		if conversation, ok := row.(*models.ReactConversation); ok {
			var visit models.StateRun
			err = e.db.WithContext(ctx).Select("id").Where("run_id = ? AND node_id = ? AND iteration = ?", runID, conversation.NodeID, conversation.Iteration).Order("id DESC").Take(&visit).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = nil
			} else if err == nil {
				extra = append(extra, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(visit.ID), 10)})
			}
		}
		if err != nil {
			result = e.db.WithContext(ctx).Session(&gorm.Session{})
			_ = result.AddError(err)
			return result
		}
		err = e.executionTransaction(ctx, runID, func(tx *gorm.DB) error {
			result = prepared.write(tx, where)
			return result.Error
		}, extra...)
	}
	if result == nil {
		result = e.db.WithContext(ctx).Session(&gorm.Session{})
	}
	if err != nil && result.Error == nil {
		_ = result.AddError(err)
	}
	return result
}

// executionMutation accepts one engine-owned UPDATE or DELETE. DryRun builds
// the statement without borrowing SQL; driver values (including legacy file
// serializers) are frozen before the guarded transaction. The raw statement
// cannot trigger serializers, callbacks, or association writes inside SQL.
func (e *Engine) executionMutation(ctx context.Context, runID string, mutation func(*gorm.DB) *gorm.DB) *gorm.DB {
	if e.execution == nil {
		return mutation(e.db.WithContext(ctx))
	}
	query := mutation(e.db.WithContext(ctx).Session(&gorm.Session{DryRun: true, SkipDefaultTransaction: true}))
	err := query.Error
	statement := query.Statement.SQL.String()
	if err == nil && !strings.HasPrefix(statement, "UPDATE ") && !strings.HasPrefix(statement, "DELETE ") {
		err = errors.New("execution mutation must contain one UPDATE or DELETE")
	}
	arguments := make([]any, len(query.Statement.Vars))
	if err == nil {
		for i, value := range query.Statement.Vars {
			if serialized, ok := value.(driver.Valuer); ok {
				arguments[i], err = serialized.Value()
			} else {
				arguments[i] = value
			}
			if err != nil {
				break
			}
		}
	}
	var result *gorm.DB
	if err == nil {
		err = e.executionTransaction(ctx, runID, func(tx *gorm.DB) error {
			result = tx.Exec(statement, arguments...)
			return result.Error
		})
	}
	if result == nil {
		result = e.db.WithContext(ctx).Session(&gorm.Session{})
	}
	if err != nil && result.Error == nil {
		_ = result.AddError(err)
	}
	return result
}
