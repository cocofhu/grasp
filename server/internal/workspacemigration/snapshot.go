// Package workspacemigration implements the offline, one-time transition to a
// single workspace. The application never reads migration reports or journals.
package workspacemigration

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Cell retains driver types: a JSON-only dump would corrupt binary secrets,
// timestamps, and integer IDs when restored through float64.
type Cell struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

type TableSnapshot struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    [][]Cell `json:"rows"`
}

type Snapshot struct {
	Tables []TableSnapshot `json:"tables"`
}

func capture(db *gorm.DB) (Snapshot, error) {
	names, err := db.Migrator().GetTables()
	if err != nil {
		return Snapshot{}, err
	}
	sort.Strings(names)
	out := Snapshot{}
	for _, name := range names {
		if !identifier.MatchString(name) {
			return out, fmt.Errorf("unsupported database identifier in migration")
		}
		if name == "sqlite_sequence" {
			continue
		}
		rows, err := db.Table(name).Rows()
		if err != nil {
			return out, err
		}
		cols, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			return out, err
		}
		table := TableSnapshot{Name: name, Columns: cols, Rows: [][]Cell{}}
		for rows.Next() {
			values := make([]any, len(cols))
			pointers := make([]any, len(cols))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				return out, err
			}
			cells := make([]Cell, len(values))
			for i, value := range values {
				cells[i], err = encodeCell(value)
				if err != nil {
					_ = rows.Close()
					return out, err
				}
			}
			table.Rows = append(table.Rows, cells)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return out, err
		}
		// The physical query order must not change the preflight fingerprint.
		sort.Slice(table.Rows, func(i, j int) bool {
			a, _ := json.Marshal(table.Rows[i])
			b, _ := json.Marshal(table.Rows[j])
			return string(a) < string(b)
		})
		out.Tables = append(out.Tables, table)
	}
	return out, nil
}

func encodeCell(v any) (Cell, error) {
	switch v := v.(type) {
	case nil:
		return Cell{Kind: "null"}, nil
	case string:
		return Cell{Kind: "string", Value: v}, nil
	case []byte:
		return Cell{Kind: "bytes", Value: base64.StdEncoding.EncodeToString(v)}, nil
	case int64:
		return Cell{Kind: "int", Value: strconv.FormatInt(v, 10)}, nil
	case uint64:
		return Cell{Kind: "uint", Value: strconv.FormatUint(v, 10)}, nil
	case float64:
		return Cell{Kind: "float", Value: strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case bool:
		return Cell{Kind: "bool", Value: strconv.FormatBool(v)}, nil
	case time.Time:
		return Cell{Kind: "time", Value: v.Format(time.RFC3339Nano)}, nil
	default:
		return Cell{}, fmt.Errorf("unsupported database value type %T", v)
	}
}

func decodeCell(c Cell) (any, error) {
	switch c.Kind {
	case "null":
		return nil, nil
	case "string":
		return c.Value, nil
	case "bytes":
		return base64.StdEncoding.DecodeString(c.Value)
	case "int":
		return strconv.ParseInt(c.Value, 10, 64)
	case "uint":
		return strconv.ParseUint(c.Value, 10, 64)
	case "float":
		return strconv.ParseFloat(c.Value, 64)
	case "bool":
		return strconv.ParseBool(c.Value)
	case "time":
		return time.Parse(time.RFC3339Nano, c.Value)
	default:
		return nil, fmt.Errorf("unsupported backup cell kind")
	}
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// A crash between two MySQL ALTERs may happen before schema-ready is recorded.
// Accept only the two known nullable additions, with every original cell intact.
func originalDataUnchanged(before, after Snapshot) bool {
	if len(before.Tables) != len(after.Tables) {
		return false
	}
	for i, old := range before.Tables {
		current := after.Tables[i]
		if old.Name != current.Name || len(old.Rows) != len(current.Rows) {
			return false
		}
		indices := map[string]int{}
		for j, column := range current.Columns {
			indices[column] = j
		}
		for _, column := range old.Columns {
			if _, ok := indices[column]; !ok {
				return false
			}
		}
		oldColumns := map[string]bool{}
		for _, column := range old.Columns {
			oldColumns[column] = true
		}
		for j, column := range current.Columns {
			if oldColumns[column] {
				continue
			}
			if !(old.Name == "workflow_defs" && column == "defaults" || old.Name == "project_credentials" && column == "builtin_slot") {
				return false
			}
			for _, row := range current.Rows {
				if row[j].Kind != "null" && row[j].Value != "" {
					return false
				}
			}
		}
		rows := map[string]int{}
		for _, row := range old.Rows {
			rows[digest(row)]++
		}
		for _, row := range current.Rows {
			projection := make([]Cell, len(old.Columns))
			for j, column := range old.Columns {
				projection[j] = row[indices[column]]
			}
			key := digest(projection)
			if rows[key] == 0 {
				return false
			}
			rows[key]--
		}
	}
	return true
}

func restore(db *gorm.DB, source Snapshot) (result error) {
	// The offline CLI uses one database connection. FK toggles must be outside
	// the data transaction (SQLite ignores the pragma inside a transaction).
	off, on := "PRAGMA foreign_keys = OFF", "PRAGMA foreign_keys = ON"
	if db.Dialector.Name() == "mysql" {
		off, on = "SET FOREIGN_KEY_CHECKS = 0", "SET FOREIGN_KEY_CHECKS = 1"
	}
	if err := db.Exec(off).Error; err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Exec(on).Error) }()
	return db.Transaction(func(tx *gorm.DB) error {
		for _, table := range source.Tables {
			if !identifier.MatchString(table.Name) {
				return fmt.Errorf("invalid backup table")
			}
			if err := tx.Exec("DELETE FROM `" + table.Name + "`").Error; err != nil {
				return err
			}
		}
		for _, table := range source.Tables {
			for _, row := range table.Rows {
				if len(row) != len(table.Columns) {
					return fmt.Errorf("invalid backup row")
				}
				values := make(map[string]any, len(row))
				for i, cell := range row {
					if !identifier.MatchString(table.Columns[i]) {
						return fmt.Errorf("invalid backup column")
					}
					v, err := decodeCell(cell)
					if err != nil {
						return err
					}
					values[table.Columns[i]] = v
				}
				if err := tx.Table(table.Name).Create(values).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
