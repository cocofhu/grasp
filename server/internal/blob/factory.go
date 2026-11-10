package blob

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/objectstore"
)

// NewFromConfig builds a Store from storage config. Unsupported drivers error.
func NewFromConfig(cfg *config.Config) (Store, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Storage.Driver))
	if driver == "" {
		driver = "local"
	}
	switch driver {
	case "local":
		root := strings.TrimSpace(cfg.Storage.BlobsRoot)
		if root == "" {
			root = "data/blobs"
		}
		return NewLocalFS(root)
	case "s3", "cos":
		options := cfg.Storage.S3
		if options.PathStyle == nil && driver == "cos" {
			pathStyle := false
			options.PathStyle = &pathStyle
		}
		objects, err := objectstore.NewS3(options)
		if err != nil {
			return nil, err
		}
		return NewWithObjectStore(cfg, objects)
	default:
		return nil, fmt.Errorf("unknown storage.driver %q", driver)
	}
}

// NewWithObjectStore reuses the same backend configured for file payloads and
// snapshots. The local directory is only a legacy read source in remote mode.
func NewWithObjectStore(cfg *config.Config, objects objectstore.Store) (Store, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	root := strings.TrimSpace(cfg.Storage.BlobsRoot)
	if root == "" {
		root = "data/blobs"
	}
	legacy, err := NewLocalFS(root)
	if err != nil {
		return nil, err
	}
	if objects == nil {
		return legacy, nil
	}
	return NewObjects(objects, legacy, cfg.Storage.MaxFileBytes)
}
