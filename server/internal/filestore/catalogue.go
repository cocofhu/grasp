package filestore

import (
	"fmt"

	"gorm.io/gorm/schema"
)

// PayloadField describes SQL-owned content without registering an ORM hook.
// Explicit raw fields contain references only; their encoding is part of the
// owning schema. Legacy serializer fields retain their historical scan rules.
type PayloadField struct {
	Column     string
	Encoding   string
	Serializer string
	Explicit   bool
}

// PayloadFields preserves schema order, so legacy backup reference hashes do
// not change when callers adopt explicit raw references in existing columns.
func PayloadFields(model *schema.Schema) ([]PayloadField, error) {
	var fields []PayloadField
	for _, field := range model.Fields {
		if field.DBName == "" || !field.Readable {
			continue
		}
		serializer, encoding := field.TagSettings["SERIALIZER"], ""
		switch serializer {
		case "filetext":
			encoding = Text
		case "filejson":
			encoding = JSON
		case "filebytes":
			encoding = Bytes
		}
		explicit := field.TagSettings["PAYLOAD"]
		if explicit != "" {
			switch explicit {
			case Text, JSON, Bytes, Binary, Image:
			default:
				return nil, fmt.Errorf("unknown payload encoding on %s.%s", model.Table, field.DBName)
			}
			if serializer != "" && encoding != explicit {
				return nil, fmt.Errorf("conflicting payload descriptions on %s.%s", model.Table, field.DBName)
			}
			encoding = explicit
		}
		if serializer != "" || explicit != "" {
			fields = append(fields, PayloadField{Column: field.DBName, Encoding: encoding, Serializer: serializer, Explicit: explicit != ""})
		}
	}
	return fields, nil
}
