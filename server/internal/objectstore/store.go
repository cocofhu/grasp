// Package objectstore supplies a common byte-storage contract for payloads,
// attachments and workspace snapshots. Callers authorize the owning SQL record;
// an object key is never a capability or a public download URL.
//
// This is the lowest storage layer (local disk, S3, replicas). filestore builds
// content-addressed payload references on top of it, and payloadstore wires
// both to the configured primary at startup.
package objectstore

import (
	"context"
	"io"
)

type Info struct {
	Key         string
	Size        int64
	ContentType string
	Metadata    map[string]string
	ETag        string
	VersionID   string
}

type Page struct {
	Objects []Info
	Next    string
}

// Store uses logical keys relative to its configured, private root prefix.
// Put receives a staged, seekable body; its size and hash are known before the
// SQL reference can be committed. Failed puts must never publish a reference.
type Store interface {
	Put(context.Context, string, io.ReadSeeker, Info) error
	Open(context.Context, string) (io.ReadCloser, Info, error)
	Head(context.Context, string) (Info, error)
	Delete(context.Context, string) error
	List(context.Context, string, string) (Page, error)
}
