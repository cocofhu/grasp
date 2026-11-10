package handlers

import "time"

const (
	// Confirm, release and restore requests carry identifiers only.
	workspaceControlBody = 8 << 10
	// Import and binding requests also carry the selected repository list.
	workspaceSelectionBody = 64 << 10
	// Snapshot admission carries a bounded job body plus its envelope.
	workspaceSnapshotBody = 72 << 10

	// Inventory and reconcile read the source workspace; preview and confirm
	// also pack or apply repository archives.
	workspaceImportReadTimeout  = 2 * time.Minute
	workspaceImportWriteTimeout = 3 * time.Minute
)
