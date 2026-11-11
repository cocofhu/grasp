package blob

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
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
	case "cos":
		return nil, fmt.Errorf("storage.driver=cos is not implemented yet; use local")
	default:
		return nil, fmt.Errorf("unknown storage.driver %q", driver)
	}
}
