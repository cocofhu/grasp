package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProbeSeparation catches two endpoint aliases reaching overlapping physical
// roots. Otherwise copying into a descendant of the source could recursively
// include the newly copied objects in later source pages. Only our random
// probe is created/deleted; no preexisting object is changed.
func ProbeSeparation(ctx context.Context, source, destination Store, sourcePrefix, destinationPrefix string) (err error) {
	root := strings.Trim(sourcePrefix, "/")
	target := strings.Trim(destinationPrefix, "/")
	physical := target
	if root == target || root == "" || strings.HasPrefix(target, root+"/") {
		physical = target
	} else if target == "" || strings.HasPrefix(root, target+"/") {
		physical = root
	} else {
		return nil // disjoint prefixes remain disjoint even through aliases
	}
	body := "grasp-replica-separation-" + uuid.NewString()
	if physical != "" {
		physical += "/"
	}
	physical += body
	logical := func(prefix string) string {
		if prefix == "" {
			return physical
		}
		return strings.TrimPrefix(physical, prefix+"/")
	}
	sourceKey, targetKey := logical(root), logical(target)
	if _, headErr := destination.Head(ctx, targetKey); !errors.Is(headErr, os.ErrNotExist) {
		return errors.New("replica separation probe could not reserve a new object")
	}
	h := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(h[:])
	if err := destination.Put(ctx, targetKey, strings.NewReader(body), Info{Size: int64(len(body)), ContentType: "application/octet-stream", Metadata: map[string]string{"grasp-sha256": digest}}); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if deleteErr := destination.Delete(cleanup, targetKey); deleteErr != nil {
			err = errors.Join(err, errors.New("replica separation probe cleanup failed"))
		}
	}()
	_, err = source.Head(ctx, sourceKey)
	if err == nil {
		return errors.New("source and destination resolve to overlapping storage roots")
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
