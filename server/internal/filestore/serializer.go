package filestore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/schema"
)

const pluginName = "grasp-file-payloads"

type storeContextKey struct{}
type referenceContextKey struct{}

// WithReferences is for platform-generated references, never raw tool input.
func WithReferences(ctx context.Context) context.Context {
	return context.WithValue(ctx, referenceContextKey{}, true)
}

func init() {
	schema.RegisterSerializer("filetext", payloadSerializer{})
	schema.RegisterSerializer("filejson", payloadSerializer{json: true})
	schema.RegisterSerializer("filebytes", payloadSerializer{bytes: true})
}

type plugin struct{ store *Store }

func (p *plugin) Name() string { return pluginName }
func (p *plugin) Initialize(db *gorm.DB) error {
	bind := func(tx *gorm.DB) {
		tx.Statement.Context = context.WithValue(tx.Statement.Context, storeContextKey{}, p.store)
	}
	for _, register := range []func(string, func(*gorm.DB)) error{
		db.Callback().Create().Before("gorm:before_create").Register,
		db.Callback().Update().Before("gorm:before_update").Register,
		db.Callback().Query().Before("gorm:query").Register,
		db.Callback().Row().Before("gorm:row").Register,
	} {
		if err := register(pluginName, bind); err != nil {
			return err
		}
	}
	// GORM's map Updates path bypasses field serializers. Build assignments
	// while they still contain logical values, then externalize SQL values.
	return db.Callback().Update().Before("gorm:update").After("gorm:save_before_associations").Register(pluginName+"-maps", func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Schema == nil {
			return
		}
		if _, ok := tx.Statement.Dest.(map[string]any); !ok {
			return
		}
		assignments := callbacks.ConvertToAssignments(tx.Statement)
		for i := range assignments {
			field := tx.Statement.Schema.LookUpField(assignments[i].Column.Name)
			if field == nil {
				continue
			}
			name := field.TagSettings["SERIALIZER"]
			if name != "filetext" && name != "filejson" && name != "filebytes" {
				continue
			}
			value := assignments[i].Value
			if name == "filejson" {
				if raw, ok := value.(string); ok && json.Valid([]byte(raw)) && field.FieldType.Kind() != reflect.Interface {
					var decoded any
					if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
						_ = tx.AddError(err)
						return
					}
					value = decoded
				}
			}
			stored, err := (payloadSerializer{json: name == "filejson", bytes: name == "filebytes"}).Value(tx.Statement.Context, field, tx.Statement.ReflectValue, value)
			if err != nil {
				_ = tx.AddError(err)
				return
			}
			assignments[i].Value = stored
		}
		if len(assignments) > 0 {
			tx.Statement.AddClause(assignments)
		}
	})
}

func Attach(db *gorm.DB, root string) error {
	if _, ok := db.Config.Plugins[pluginName]; ok {
		return nil
	}
	s, err := New(root)
	if err != nil {
		return err
	}
	return db.Use(&plugin{store: s})
}

// Rebind is only valid during startup, before this DB is shared with workers.
func Rebind(db *gorm.DB, root string) error {
	if p, ok := db.Config.Plugins[pluginName].(*plugin); ok {
		s, err := New(root)
		if err != nil {
			return err
		}
		p.store = s
		return nil
	}
	return Attach(db, root)
}

func FromDB(db *gorm.DB) (*Store, error) {
	p, ok := db.Config.Plugins[pluginName].(*plugin)
	if !ok {
		return nil, errors.New("file payload storage is not configured")
	}
	return p.store, nil
}

func fromContext(ctx context.Context) (*Store, error) {
	s, ok := ctx.Value(storeContextKey{}).(*Store)
	if !ok {
		return nil, errors.New("file payload storage is not configured")
	}
	return s, nil
}

type payloadSerializer struct {
	json  bool
	bytes bool
}

func rawString(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case []byte:
		return string(x), nil
	default:
		return "", fmt.Errorf("unexpected payload type %T", v)
	}
}

func setRef(dst reflect.Value, ref string) {
	v := reflect.Indirect(dst)
	if v.Kind() != reflect.Struct {
		return
	}
	f := v.FieldByName("PathRef")
	if f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
		f.SetString(ref)
	}
}

func recordKind(dst reflect.Value) string {
	v := reflect.Indirect(dst)
	if v.Kind() == reflect.Struct {
		f := v.FieldByName("Kind")
		if f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
	}
	return ""
}

func (p payloadSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	raw, err := rawString(dbValue)
	if err != nil {
		return err
	}
	if IsRef(raw) {
		encoding, _, _ := Parse(raw)
		expected := Text
		if p.json {
			expected = JSON
		} else if p.bytes {
			expected = Bytes
		}
		artifact := field.Schema.Table == "artifacts" || field.Schema.Table == "artifact_versions"
		kind := recordKind(dst)
		image := artifact && expected == Text && kind == "image" && encoding == Image
		binary := artifact && expected == Text && (kind == "file" || kind == "pdf" || kind == "audio" || kind == "video") && encoding == Binary
		if encoding != expected && !image && !binary {
			return errors.New("stored payload encoding disagrees with its owner")
		}
		s, err := fromContext(ctx)
		if err != nil {
			return err
		}
		setRef(dst, raw)
		if encoding == Binary {
			return field.Set(ctx, dst, raw)
		}
		data, err := s.Read(ctx, raw)
		if err != nil {
			return err
		}
		if encoding == Image {
			raw = base64.StdEncoding.EncodeToString(data)
		} else {
			raw = string(data)
		}
	} else if strings.HasPrefix(raw, Prefix) {
		return errors.New("invalid stored file reference")
	} else if raw != "" && !(p.json && raw == "null") {
		return ErrNonCanonicalPayload
	}
	if p.bytes {
		return field.Set(ctx, dst, []byte(raw))
	}
	if !p.json {
		return field.Set(ctx, dst, raw)
	}
	v := reflect.New(field.FieldType)
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), v.Interface()); err != nil {
			return err
		}
	}
	field.ReflectValueOf(ctx, dst).Set(v.Elem())
	return nil
}

func (p payloadSerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, value any) (any, error) {
	var err error
	encoding := Text
	if p.bytes {
		encoding = Bytes
	}
	var data []byte
	if p.json {
		data, err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if string(data) == "null" {
			return nil, nil
		}
		encoding = JSON
	} else {
		raw, err := rawString(value)
		if err != nil {
			return nil, err
		}
		if raw == "" {
			return "", nil
		}
		kind := recordKind(dst)
		allowRef, _ := ctx.Value(referenceContextKey{}).(bool)
		if IsRef(raw) && allowRef {
			s, err := fromContext(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Verify(ctx, raw); err != nil {
				return nil, err
			}
			return raw, nil
		}
		data = []byte(raw)
		if kind == "image" && (field.Schema.Table == "artifacts" || field.Schema.Table == "artifact_versions") {
			data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
			if err != nil {
				return nil, fmt.Errorf("decode image payload: %w", err)
			}
			encoding = Image
		}
	}
	s, err := fromContext(ctx)
	if err != nil {
		return nil, err
	}
	ref, _, err := s.Put(ctx, bytes.NewReader(data), encoding)
	return ref, err
}
