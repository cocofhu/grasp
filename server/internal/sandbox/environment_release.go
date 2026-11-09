package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
)

const EnvironmentReleaseVersion = "kubernetes-uid-delete-v1"

type EnvironmentReleaseObject struct {
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name"`
	UID         string `json:"uid"`
	ObserveOnly bool   `json:"observeOnly,omitempty"`
}
type EnvironmentReleasePlan struct {
	Version   string                     `json:"version"`
	Resource  string                     `json:"resource"`
	Namespace string                     `json:"namespace"`
	Objects   []EnvironmentReleaseObject `json:"objects"`
	SHA256    string                     `json:"sha256"`
}

func (p EnvironmentReleasePlan) Digest() string {
	p.SHA256 = ""
	p.Objects = append([]EnvironmentReleaseObject(nil), p.Objects...)
	sort.Slice(p.Objects, func(i, j int) bool {
		a, b := p.Objects[i], p.Objects[j]
		return a.Kind+"\x00"+a.Namespace+"\x00"+a.Name < b.Kind+"\x00"+b.Namespace+"\x00"+b.Name
	})
	body, _ := json.Marshal(p)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (p EnvironmentReleasePlan) Valid() bool {
	if p.Version != EnvironmentReleaseVersion || p.Resource == "" || len(p.Resource) > 128 || strings.ContainsAny(p.Resource, "/\\\x00\n\r") || p.Namespace == "" || len(p.Objects) < 3 || len(p.Objects) > 128 || p.SHA256 != p.Digest() {
		return false
	}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, o := range p.Objects {
		if o.Name == "" || o.UID == "" || len(o.Name) > 253 || len(o.UID) > 128 || strings.ContainsAny(o.Name+o.UID, "\x00\n\r/") {
			return false
		}
		switch o.Kind {
		case "Deployment", "PersistentVolumeClaim", "Service", "Secret", "ReplicaSet", "Pod":
			if o.Namespace != p.Namespace || o.ObserveOnly {
				return false
			}
		case "PersistentVolume":
			if o.Namespace != "" || !o.ObserveOnly {
				return false
			}
		default:
			return false
		}
		key := o.Kind + "\x00" + o.Namespace + "\x00" + o.Name
		if seen[key] {
			return false
		}
		seen[key] = true
		counts[o.Kind]++
	}
	return counts["Deployment"] == 1 && counts["PersistentVolumeClaim"] == 1 && counts["PersistentVolume"] == 1
}

type EnvironmentReleaseCapabilities struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	Version   string `json:"version,omitempty"`
}
type EnvironmentReleasePrepare struct {
	ID      string `json:"id"`
	Lineage string `json:"lineage"`
}
type EnvironmentReleaseConfirm struct {
	PlanSHA256   string    `json:"planSha256"`
	Nonce        string    `json:"nonce"`
	Confirmed    bool      `json:"confirmed"`
	LossAccepted bool      `json:"lossAccepted"`
	Owner        string    `json:"owner"`
	Generation   uint64    `json:"generation"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Deadline     time.Time `json:"deadline"`
}
type EnvironmentReleaseRenew struct {
	Owner      string    `json:"owner"`
	Generation uint64    `json:"generation"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type EnvironmentReleaseReceipt struct {
	ID         string                     `json:"id"`
	Resource   string                     `json:"resource"`
	Lineage    string                     `json:"lineage"`
	PlanSHA256 string                     `json:"planSha256"`
	Objects    []EnvironmentReleaseObject `json:"objects"`
	VerifiedAt time.Time                  `json:"verifiedAt"`
}
type EnvironmentReleaseView struct {
	ID       string                     `json:"id"`
	State    string                     `json:"state"`
	Plan     EnvironmentReleasePlan     `json:"plan"`
	Receipt  *EnvironmentReleaseReceipt `json:"receipt,omitempty"`
	Error    string                     `json:"error,omitempty"`
	Deadline time.Time                  `json:"deadline"`
}

func releasePath(id string) string { return "/api/v1/sandboxes/" + url.PathEscape(id) + "/releases" }
func (g *GatewayClient) EnvironmentReleaseCapabilities(ctx context.Context, id string) (EnvironmentReleaseCapabilities, error) {
	var out EnvironmentReleaseCapabilities
	err := g.do(ctx, "GET", releasePath(id)+"/capabilities", nil, &out)
	return out, err
}
func (g *GatewayClient) PreviewEnvironmentRelease(ctx context.Context, id string, in EnvironmentReleasePrepare) (EnvironmentReleaseView, error) {
	var out EnvironmentReleaseView
	err := g.do(ctx, "POST", releasePath(id), in, &out)
	if err == nil {
		err = g.validateEnvironmentRelease(id, out)
	}
	return out, err
}
func (g *GatewayClient) ConfirmEnvironmentRelease(ctx context.Context, id, releaseID string, in EnvironmentReleaseConfirm) (EnvironmentReleaseView, error) {
	var out EnvironmentReleaseView
	err := g.do(ctx, "POST", releasePath(id)+"/"+url.PathEscape(releaseID)+"/confirm", in, &out)
	if err == nil {
		err = g.validateEnvironmentRelease(id, out)
	}
	return out, err
}
func (g *GatewayClient) RenewEnvironmentRelease(ctx context.Context, id, releaseID string, in EnvironmentReleaseRenew) (EnvironmentReleaseView, error) {
	var out EnvironmentReleaseView
	err := g.do(ctx, "PATCH", releasePath(id)+"/"+url.PathEscape(releaseID)+"/fence", in, &out)
	if err == nil {
		err = g.validateEnvironmentRelease(id, out)
	}
	return out, err
}
func (g *GatewayClient) GetEnvironmentRelease(ctx context.Context, id, releaseID string) (EnvironmentReleaseView, error) {
	var out EnvironmentReleaseView
	err := g.do(ctx, "GET", releasePath(id)+"/"+url.PathEscape(releaseID), nil, &out)
	if err == nil {
		err = g.validateEnvironmentRelease(id, out)
	}
	return out, err
}

func (g *GatewayClient) validateEnvironmentRelease(id string, out EnvironmentReleaseView) error {
	_, native, err := g.resolveID(id)
	if err != nil {
		return err
	}
	if out.Plan.Resource != native || !out.Plan.Valid() {
		return errors.New("gateway environment release resource proof mismatch")
	}
	if out.Receipt != nil && (out.Receipt.Resource != native || out.Receipt.ID != out.ID || out.Receipt.PlanSHA256 != out.Plan.SHA256) {
		return errors.New("gateway environment release receipt mismatch")
	}
	return nil
}
