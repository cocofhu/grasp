package driver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

var (
	ErrEnvironmentReleaseUnsupported = errors.New("verified environment release unsupported")
	ErrEnvironmentReleaseConflict    = errors.New("environment release incarnation changed")
	ErrEnvironmentReleasePending     = errors.New("environment release pending confirmation")
)

const EnvironmentReleaseVersion = "kubernetes-uid-delete-v1"

// EnvironmentReleaseObject is an immutable physical identity, not a browser
// supplied path. A PV is observed only; its PVC's Delete policy owns reclamation.
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

// VerifiedEnvironmentReleaser is optional. Legacy best-effort Destroy is not
// evidence of physical deletion and cannot implement this protocol by itself.
// Guard must be checked before every native mutation. Expiry may leave a
// partial scope; neither Check nor Gone is permitted to replay those mutations.
type VerifiedEnvironmentReleaser interface {
	PreviewEnvironmentRelease(context.Context, string) (EnvironmentReleasePlan, error)
	CheckEnvironmentRelease(context.Context, EnvironmentReleasePlan) error
	DeleteEnvironmentRelease(context.Context, EnvironmentReleasePlan, func(context.Context) error) error
	EnvironmentReleaseGone(context.Context, EnvironmentReleasePlan) (bool, error)
}
