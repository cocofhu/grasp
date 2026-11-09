package executionlineage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/objectstore"
)

// OpenConfigured requires an independent control location. protected contains
// SQL/backup directories; payloads includes both the active and replica object
// namespaces. Neither an alias nor a child of a backed-up root is independent.
// The returned authority still needs an explicit offline initialization.
func OpenConfigured(ctx context.Context, control config.StorageConfig, payloads []config.StorageConfig, protected []string) (*Authority, error) {
	driver := strings.ToLower(strings.TrimSpace(control.Driver))
	if driver == "local" {
		if strings.TrimSpace(control.BlobsRoot) == "" {
			return nil, errors.New("execution control requires an explicit independent local root")
		}
		root, err := physicalPath(control.BlobsRoot)
		if err != nil {
			return nil, errors.New("could not resolve independent execution control root")
		}
		paths := append([]string(nil), protected...)
		for _, payload := range payloads {
			path := payload.BlobsRoot
			if path == "" {
				path = "data/blobs"
			}
			paths = append(paths, path) // S3 installations also retain local legacy bytes.
		}
		for _, path := range paths {
			if path == "" {
				continue
			}
			other, err := physicalPath(path)
			if err != nil {
				return nil, errors.New("could not resolve protected SQL or payload root")
			}
			if pathOverlap(root, other) {
				return nil, errors.New("execution control root overlaps SQL, payload or backup storage")
			}
		}
		store, err := objectstore.NewLocalControl(root)
		if err != nil {
			return nil, err
		}
		return Open(ctx, store)
	}
	if driver != "s3" && driver != "cos" {
		return nil, errors.New("execution control requires an explicit local, s3 or cos driver")
	}
	store, err := objectstore.NewS3(control.S3)
	if err != nil {
		return nil, err
	}
	for _, payload := range payloads {
		if payload.Driver != "s3" && payload.Driver != "cos" {
			continue
		}
		if strings.TrimRight(control.S3.Endpoint, "/") == strings.TrimRight(payload.S3.Endpoint, "/") && control.S3.Bucket == payload.S3.Bucket && prefixOverlap(control.S3.Prefix, payload.S3.Prefix) {
			return nil, errors.New("execution control namespace overlaps a payload or replica namespace")
		}
		// Distinct endpoint spellings can resolve to the same bucket. A real
		// isolated probe is necessary whenever the logical prefixes overlap.
		if prefixOverlap(control.S3.Prefix, payload.S3.Prefix) {
			objects, err := objectstore.NewS3(payload.S3)
			if err != nil {
				return nil, err
			}
			if err := objectstore.ProbeSeparation(ctx, objects, store, payload.S3.Prefix, control.S3.Prefix); err != nil {
				return nil, errors.Join(ErrUnavailable, err)
			}
		}
	}
	return Open(ctx, store)
}

func prefixOverlap(a, b string) bool {
	a, b = strings.Trim(a, "/"), strings.Trim(b, "/")
	return a == b || a == "" || b == "" || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func pathOverlap(a, b string) bool {
	within := func(root, other string) bool {
		rel, err := filepath.Rel(root, other)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	}
	return within(a, b) || within(b, a)
}

// Resolve symlinked existing ancestors even when the final control directory
// does not exist yet. Lexical comparisons alone miss aliases into backups.
func physicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current, suffix := abs, []string{}
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(current) == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = filepath.Dir(current)
	}
}
