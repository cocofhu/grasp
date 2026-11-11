package blob

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// IngestPromptImages externalizes any image with inline Data into store,
// clears Data, and fills Ref / SizeBytes. Images that already have Ref are
// left alone (Data still stripped). Nil store with inline Data is an error.
func IngestPromptImages(ctx context.Context, store Store, images []models.PromptImage) ([]models.PromptImage, error) {
	if len(images) == 0 {
		return images, nil
	}
	out := make([]models.PromptImage, len(images))
	for i, im := range images {
		out[i] = im
		if ref := strings.TrimSpace(im.Ref); ref != "" {
			if _, err := ParseRef(ref); err != nil {
				return nil, fmt.Errorf("image[%d]: %w", i, err)
			}
			out[i].Ref = ref
			out[i].Data = ""
			continue
		}
		data := strings.TrimSpace(im.Data)
		if data == "" {
			return nil, fmt.Errorf("image[%d]: missing data and ref", i)
		}
		if store == nil {
			return nil, fmt.Errorf("image[%d]: blob store not configured", i)
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("image[%d]: decode base64: %w", i, err)
		}
		ref, err := store.Put(ctx, bytes.NewReader(raw), Meta{
			MimeType: im.MimeType,
			Name:     im.Name,
		})
		if err != nil {
			return nil, fmt.Errorf("image[%d]: store: %w", i, err)
		}
		out[i].Ref = ref.String()
		out[i].Data = ""
		out[i].SizeBytes = int64(len(raw))
		if out[i].MimeType == "" {
			out[i].MimeType = "application/octet-stream"
		}
	}
	return out, nil
}

// IngestCompositeInputs walks a launch inputs map and externalizes any
// composite {text, images[]} values in place. Returns the same map.
func IngestCompositeInputs(ctx context.Context, store Store, inputs map[string]any) (map[string]any, error) {
	if inputs == nil {
		return inputs, nil
	}
	for k, v := range inputs {
		nv, err := ingestValue(ctx, store, v)
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", k, err)
		}
		inputs[k] = nv
	}
	return inputs, nil
}

func ingestValue(ctx context.Context, store Store, v any) (any, error) {
	if ct := models.AsCompositeText(v); ct != nil && len(ct.Images) > 0 {
		imgs, err := IngestPromptImages(ctx, store, ct.Images)
		if err != nil {
			return nil, err
		}
		ct.Images = imgs
		return map[string]any{
			"text":   ct.Text,
			"images": promptImagesToAny(imgs),
		}, nil
	}
	return v, nil
}

func promptImagesToAny(imgs []models.PromptImage) []any {
	out := make([]any, len(imgs))
	for i, im := range imgs {
		m := map[string]any{"mimeType": im.MimeType}
		if im.Ref != "" {
			m["ref"] = im.Ref
		}
		if im.Name != "" {
			m["name"] = im.Name
		}
		if im.SizeBytes > 0 {
			m["sizeBytes"] = im.SizeBytes
		}
		out[i] = m
	}
	return out
}

// ResolveForWire returns copies with Data loaded from store for ACP/LLM transport.
func ResolveForWire(ctx context.Context, store Store, images []models.PromptImage) ([]models.PromptImage, error) {
	if len(images) == 0 {
		return images, nil
	}
	out := make([]models.PromptImage, 0, len(images))
	for i, im := range images {
		ref := strings.TrimSpace(im.Ref)
		if ref == "" {
			continue
		}
		if store == nil {
			return nil, fmt.Errorf("image[%d]: blob store not configured to resolve %s", i, ref)
		}
		parsed, err := ParseRef(ref)
		if err != nil {
			return nil, fmt.Errorf("image[%d]: %w", i, err)
		}
		rc, meta, err := store.Open(ctx, parsed)
		if err != nil {
			return nil, fmt.Errorf("image[%d]: open %s: %w", i, ref, err)
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("image[%d]: read %s: %w", i, ref, err)
		}
		mime := im.MimeType
		if mime == "" {
			mime = meta.MimeType
		}
		name := im.Name
		if name == "" {
			name = meta.Name
		}
		out = append(out, models.PromptImage{
			Data:      base64.StdEncoding.EncodeToString(raw),
			MimeType:  mime,
			Name:      name,
			Ref:       ref,
			SizeBytes: int64(len(raw)),
		})
	}
	return out, nil
}

// StripData clears inline Data before persist; images are always Ref-backed.
func StripData(images []models.PromptImage) []models.PromptImage {
	if len(images) == 0 {
		return images
	}
	out := make([]models.PromptImage, len(images))
	for i, im := range images {
		out[i] = im
		out[i].Data = ""
	}
	return out
}

// StripDataInValue clears PromptImage.Data inside composite map/struct values.
func StripDataInValue(v any) any {
	if ct := models.AsCompositeText(v); ct != nil && len(ct.Images) > 0 {
		return map[string]any{
			"text":   ct.Text,
			"images": promptImagesToAny(StripData(ct.Images)),
		}
	}
	return v
}
