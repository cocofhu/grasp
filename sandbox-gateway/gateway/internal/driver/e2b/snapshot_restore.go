package e2b

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const restoreLabel = "grasp.gateway.restore"

// A restore creates a new provider ID. Persist the attempt before POST so a
// lost response is reconciled by owner metadata, never by a second creation.
type snapshotRestore struct {
	Token      string `json:"token"`
	SnapshotID string `json:"snapshotID"`
	SourceID   string `json:"sourceID"`
	SandboxID  string `json:"sandboxID,omitempty"`
	Dispatched bool   `json:"dispatched,omitempty"`
}

func latestRecoveryPoint(r *record) *recoveryPoint {
	for i := range r.RecoveryPoints {
		p := &r.RecoveryPoints[i]
		if p.ID == r.LatestSnapshotID && p.Ready && !p.Deleting {
			return p
		}
	}
	return nil
}

func (d *Driver) restoreMetadata(r *record) map[string]string {
	metadata := make(map[string]string, len(r.Provider.Metadata)+1)
	for k, v := range r.Provider.Metadata {
		metadata[k] = v
	}
	metadata[ownerLabel], metadata[idLabel], metadata[driverLabel] = d.opts.OwnerID, r.ID, "e2b"
	metadata[restoreLabel] = r.Restore.Token
	return metadata
}

func (d *Driver) discoverRestore(ctx context.Context, r *record) error {
	if r.Restore.SandboxID != "" || !r.Restore.Dispatched {
		return nil
	}
	found, err := d.client.List(ctx, map[string]string{ownerLabel: d.opts.OwnerID, idLabel: r.ID, driverLabel: "e2b", restoreLabel: r.Restore.Token})
	if err != nil {
		return err
	}
	if len(found) != 1 {
		return errors.New("E2B snapshot restore outcome is uncertain; creation will not be repeated")
	}
	r.Restore.SandboxID = found[0].ID
	return d.save(r)
}

func (d *Driver) restoreLatest(ctx context.Context, r *record) error {
	point := latestRecoveryPoint(r)
	if point == nil {
		return errors.New("E2B latest recovery point is unavailable; refusing an older or empty sandbox")
	}
	if r.Restore == nil {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return errors.New("cannot allocate E2B restore identity")
		}
		r.Restore = &snapshotRestore{Token: hex.EncodeToString(nonce[:]), SnapshotID: point.ID, SourceID: r.Provider.ID}
		if err := d.save(r); err != nil {
			return err
		}
	}
	if err := d.discoverRestore(ctx, r); err != nil {
		return err
	}
	if !r.Restore.Dispatched {
		// Checkpoint ownership is pinned to the immutable template ID in the
		// private journal. The provider checks team access to the template again.
		r.Restore.Dispatched = true
		if err := d.save(r); err != nil {
			return err
		}
		s, err := d.client.Create(ctx, CreateOptions{TemplateID: r.Restore.SnapshotID + ":default", TimeoutSeconds: int(d.opts.Lease / time.Second), AutoPause: d.opts.AutoPause, Metadata: d.restoreMetadata(r)})
		if s != nil && providerIDPattern.MatchString(s.ID) {
			r.Restore.SandboxID = s.ID
		}
		var apiErr *APIError
		if s == nil && errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
			// Explicit rejection did not start a guest. Ambiguous failures remain
			// journaled and must be discovered instead of blindly replayed.
			r.Restore.Dispatched = false
		}
		if saveErr := d.save(r); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
		if s == nil {
			return errors.New("E2B snapshot restore returned no identity")
		}
	}
	current, err := d.client.Get(ctx, r.Restore.SandboxID)
	if err != nil {
		return err
	}
	template, _, _ := strings.Cut(current.TemplateID, ":")
	if !d.owns(current, r.ID) || current.Metadata[restoreLabel] != r.Restore.Token || template != r.Restore.SnapshotID || current.CPUCount != int(r.Profile.CPUCores) || current.MemoryMB != r.Profile.MemoryMB {
		return errors.New("E2B snapshot restore ownership or resources mismatch")
	}
	// Connect refreshes the private traffic/envd credentials even when the
	// create response was lost. Never reuse the source guest's tokens.
	resumed, err := d.connectForResume(ctx, current.ID)
	if err != nil {
		return err
	}
	resumed.Metadata = current.Metadata
	// Same gateway ID, new provider identity; move the reserved local ports.
	if r.Provider.ID != resumed.ID {
		_ = d.tunnels.Close(r.Provider.ID)
	}
	r.Provider = *resumed
	if err := d.save(r); err != nil {
		return err
	}
	if err := d.convergeResumed(ctx, r); err != nil {
		return err
	}
	r.Phase, r.Restore, r.LatestSnapshotID = "running", nil, ""
	return d.save(r)
}

func validateRecovery(r *record) error {
	names, ids := map[string]bool{}, map[string]bool{}
	capture, latest := r.CaptureName == "", r.LatestSnapshotID == ""
	for _, p := range r.RecoveryPoints {
		if !providerIDPattern.MatchString(p.Name) || !strings.HasPrefix(p.Name, "grasp-") || !providerIDPattern.MatchString(p.SourceID) || (p.ID != "" && !providerIDPattern.MatchString(p.ID)) || names[p.Name] || (p.ID != "" && ids[p.ID]) || (p.Ready && p.ID == "") {
			return errors.New("invalid E2B snapshot recovery identity")
		}
		names[p.Name], ids[p.ID] = true, true
		if p.Name == r.CaptureName && !p.Deleting {
			capture = true
		}
		if p.ID == r.LatestSnapshotID && p.Ready && !p.Deleting {
			latest = true
		}
	}
	if !capture || !latest || r.BackupKeep < 0 || (r.Phase == "releasing" && latestRecoveryPoint(r) == nil) {
		return errors.New("invalid E2B snapshot recovery references")
	}
	if attempt := r.Restore; attempt != nil {
		if !providerIDPattern.MatchString(attempt.Token) || !providerIDPattern.MatchString(attempt.SourceID) || attempt.SnapshotID != r.LatestSnapshotID || attempt.SnapshotID == "" || (attempt.SandboxID != "" && (!providerIDPattern.MatchString(attempt.SandboxID) || !attempt.Dispatched)) {
			return errors.New("invalid E2B snapshot restore intent")
		}
	}
	return nil
}
