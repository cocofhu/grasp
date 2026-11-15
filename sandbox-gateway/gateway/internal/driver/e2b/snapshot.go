package e2b

import (
	"context"
	"errors"
	"net/http"
	"time"

	"sandbox-gateway/internal/driver"
)

// SnapshotStatus is authoritative persistence/cleanup progress from the
// provider. Paused VM state alone does not prove an async upload has finished
// or that its process has exited and released the runtime allocation.
type SnapshotStatus struct {
	State            string `json:"state"`
	BuildID          string `json:"buildID"`
	Keep             int    `json:"keep"`
	PendingCleanup   int    `json:"pendingCleanup"`
	WorkloadReleased bool   `json:"workloadReleased"`
	LastError        string `json:"lastError"`
}

func (c *Client) SnapshotStatus(ctx context.Context, id string) (SnapshotStatus, error) {
	var status SnapshotStatus
	path, err := sandboxPath(id)
	if err != nil {
		return status, err
	}
	_, err = c.control(ctx, "snapshot status", http.MethodGet, path+"/snapshot-status", nil, nil, &status, http.StatusOK)
	return status, err
}

func (c *Client) WaitSnapshotReady(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		status, err := c.SnapshotStatus(ctx, id)
		if err != nil {
			return err
		}
		if status.State == "ready" && status.BuildID != "" && status.WorkloadReleased && status.LastError == "" {
			return nil
		}
		if status.State != "pending" || status.LastError != "" {
			// Provider error bodies may contain paths and credentials; expose a
			// fixed diagnostic while retaining the authoritative error provider-side.
			return errors.New("E2B snapshot persistence not confirmed; recovery state retained")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// deleteConfirmed keeps a durable retry record while provider GC is pending.
// A missing live VM is not evidence that its disk snapshots have been removed.
func (d *Driver) deleteConfirmed(ctx context.Context, id string) error {
	if err := d.client.Delete(ctx, id); err != nil {
		return err
	}
	status, err := d.client.SnapshotStatus(ctx, id)
	if err != nil {
		return err
	}
	if status.State != "deleted" || status.PendingCleanup != 0 || !status.WorkloadReleased {
		return &driver.CleanupPendingError{WorkloadReleased: status.WorkloadReleased}
	}
	return nil
}
