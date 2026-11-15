package e2b

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sandbox-gateway/internal/driver"
)

// recoveryPoint is an independent snapshot owned by this gateway. Journal its
// name before capture, then pin the immutable template ID before any deletion.
// Ready follows the official API contract, not a physical storage receipt.
type recoveryPoint struct {
	Name       string    `json:"name"`
	ID         string    `json:"id,omitempty"`
	SourceID   string    `json:"sourceID"`
	CreatedAt  time.Time `json:"createdAt"`
	Dispatched bool      `json:"dispatched,omitempty"`
	Ready      bool      `json:"ready,omitempty"`
	Deleting   bool      `json:"deleting,omitempty"`
}

type snapshotWire struct {
	ID    string   `json:"snapshotID"`
	Names []string `json:"names"`
}

func snapshotNamed(s snapshotWire, name string) bool {
	for _, candidate := range append([]string{s.ID}, s.Names...) {
		identifier, tag, _ := strings.Cut(candidate, ":")
		if tag != "" && tag != "default" {
			continue
		}
		parts := strings.Split(identifier, "/")
		if len(parts) <= 2 && parts[len(parts)-1] == name {
			return true
		}
	}
	return false
}

// ResolveSnapshot discovers failed/in-progress captures through their alias,
// then checks for a successful default build through the standard list API.
func (c *Client) ResolveSnapshot(ctx context.Context, source, name, pinnedID string) (string, bool, error) {
	if !providerIDPattern.MatchString(source) || !providerIDPattern.MatchString(name) ||
		(pinnedID != "" && !providerIDPattern.MatchString(pinnedID)) {
		return "", false, errors.New("invalid E2B snapshot identity")
	}
	id := pinnedID
	if id == "" {
		var alias struct {
			ID string `json:"templateID"`
		}
		_, err := c.control(ctx, "resolve snapshot", http.MethodGet, "/templates/aliases/"+name, nil, nil, &alias, http.StatusOK)
		if errors.Is(err, ErrNotFound) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		if !providerIDPattern.MatchString(alias.ID) {
			return "", false, errors.New("E2B returned an invalid snapshot identity")
		}
		id = alias.ID
	}
	var points []snapshotWire
	_, err := c.control(ctx, "list snapshot", http.MethodGet, "/snapshots",
		url.Values{"sandboxID": {source}, "name": {id + ":default"}, "limit": {"100"}}, nil, &points, http.StatusOK)
	if err != nil {
		return id, false, err
	}
	for _, point := range points {
		if snapshotNamed(point, name) || point.ID == id+":default" || point.ID == id {
			return id, true, nil
		}
	}
	return id, false, nil
}

func (c *Client) CreateSnapshot(ctx context.Context, source, name string) error {
	path, err := sandboxPath(source)
	if err != nil || !providerIDPattern.MatchString(name) {
		return errors.New("invalid E2B snapshot capture identity")
	}
	var result snapshotWire
	_, err = c.control(ctx, "create snapshot", http.MethodPost, path+"/snapshots", nil,
		map[string]any{"name": name, "memory": true}, &result, http.StatusCreated)
	if err == nil && !snapshotNamed(result, name) {
		return errors.New("E2B snapshot response identity mismatch")
	}
	return err
}

func (c *Client) DeleteSnapshot(ctx context.Context, id string) error {
	if !providerIDPattern.MatchString(id) {
		return errors.New("invalid E2B snapshot template ID")
	}
	_, err := c.control(ctx, "delete snapshot", http.MethodDelete, "/templates/"+id, nil, nil, nil, http.StatusNoContent)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (d *Driver) captureRecoveryPoint(ctx context.Context, r *record, allowCreate bool) error {
	var point *recoveryPoint
	for i := range r.RecoveryPoints {
		if r.RecoveryPoints[i].Name == r.CaptureName {
			point = &r.RecoveryPoints[i]
			break
		}
	}
	if point == nil || point.Deleting {
		return errors.New("E2B pending snapshot record is missing")
	}
	if point.Ready {
		return nil
	}
	id, ready, err := d.client.ResolveSnapshot(ctx, point.SourceID, point.Name, point.ID)
	if id != "" {
		point.ID = id
		if saveErr := d.save(r); saveErr != nil {
			return saveErr
		}
	}
	if err != nil {
		return err
	}
	if !ready && allowCreate {
		// A repeated name reuses the same template. Older official APIs do not
		// support snapshot Idempotency-Key; discover successful builds first.
		wasDispatched := point.Dispatched
		point.Dispatched = true
		if err := d.save(r); err != nil {
			return err
		}
		if err := d.client.CreateSnapshot(ctx, point.SourceID, point.Name); err != nil {
			// Only pre-execution validation/auth/rate-limit refusals resolve a first
			// attempt. A 409 may follow persisted rows in supported providers;
			// and a later rejection cannot disprove an earlier ambiguous request.
			var apiErr *APIError
			if !wasDispatched && errors.As(err, &apiErr) && snapshotRejectedBeforeCapture(apiErr.HTTPStatus) {
				point.Dispatched = false
				if saveErr := d.save(r); saveErr != nil {
					return saveErr
				}
			}
			return err
		}
		id, ready, err = d.client.ResolveSnapshot(ctx, point.SourceID, point.Name, "")
		point.ID = id
		if saveErr := d.save(r); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return err
		}
	}
	if !ready {
		return errors.New("E2B snapshot capture is pending; recovery intent retained")
	}
	point.Ready = true
	return d.save(r)
}

// A native pause consumes one point. Independent snapshots are the authoritative
// saved state after checkpoint-and-release; there is no duplicate native pause.
func (d *Driver) pruneSnapshots(ctx context.Context, r *record, keep int) error {
	remaining := keep
	if keep > 0 && r.LatestSnapshotID == "" {
		remaining--
	}
	for i := range r.RecoveryPoints {
		p := &r.RecoveryPoints[i]
		if p.Deleting || p.Name == r.CaptureName {
			continue
		}
		if p.Ready && remaining > 0 {
			remaining--
		} else {
			p.Deleting = true
		}
	}
	if err := d.save(r); err != nil {
		return err
	}
	return d.cleanupSnapshots(ctx, r)
}

func (d *Driver) cleanupSnapshots(ctx context.Context, r *record) error {
	var failures []error
	for i := 0; i < len(r.RecoveryPoints); {
		p := &r.RecoveryPoints[i]
		if !p.Deleting {
			i++
			continue
		}
		if p.ID == "" {
			id, _, err := d.client.ResolveSnapshot(ctx, p.SourceID, p.Name, "")
			if err != nil {
				failures = append(failures, err)
				i++
				continue
			}
			if id == "" && !p.Dispatched {
				r.RecoveryPoints = append(r.RecoveryPoints[:i], r.RecoveryPoints[i+1:]...)
				if err := d.save(r); err != nil {
					return err
				}
				continue
			}
			if id == "" {
				failures = append(failures, errors.New("E2B snapshot creation outcome remains uncertain"))
				i++
				continue
			}
			p.ID = id
			if err := d.save(r); err != nil {
				return err
			}
		}
		// A clone can later use native keep1 pause. Its base checkpoint is then
		// a structural dependency, no longer an independently selectable point.
		// Keep the deletion tombstone without blocking wake; remove it after the
		// last owned guest stops referring to it (in particular on cancellation).
		base, _, _ := strings.Cut(r.Provider.TemplateID, ":")
		if base == p.ID && r.Provider.ID != "" {
			current, err := d.client.Get(ctx, r.Provider.ID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				failures = append(failures, err)
				i++
				continue
			}
			if err == nil {
				if !d.owns(current, r.ID) {
					failures = append(failures, errors.New("E2B snapshot dependency ownership mismatch"))
				}
				i++
				continue
			}
		}
		if err := d.client.DeleteSnapshot(ctx, p.ID); err != nil {
			failures = append(failures, err)
			i++
			continue
		}
		r.RecoveryPoints = append(r.RecoveryPoints[:i], r.RecoveryPoints[i+1:]...)
		if err := d.save(r); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

// Confirm the provider no longer exposes the sandbox. Physical process exit and
// storage reclamation remain provider responsibilities, not private API gates.
func (d *Driver) deleteConfirmed(ctx context.Context, id string) error {
	if err := d.client.Delete(ctx, id); err != nil {
		return err
	}
	_, err := d.client.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return &driver.CleanupPendingError{}
}

func snapshotRejectedBeforeCapture(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}
