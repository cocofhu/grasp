package models

import "time"

// Share-link kinds (one table; gate vs review sessions must not mix).
const (
	ShareLinkKindHumanGate = "human_gate"
	ShareLinkKindReview    = "review"
)

// GateShareLink is a one-shot external credential bound to a single human_gate
// or inbox review instance. Only the SHA-256 hash of the token is persisted.
// Review rows keep GateID nil (no fake Gate); human_gate rows always set it.
type GateShareLink struct {
	ID        string `gorm:"primaryKey;size:40" json:"id"`
	TokenHash string `gorm:"uniqueIndex;size:64;not null" json:"-"`
	Kind      string `gorm:"size:32;index:idx_gsl_kind_instance,priority:1;not null" json:"kind"`
	RunID     string `gorm:"index:idx_gsl_instance,priority:1;index:idx_gsl_kind_instance,priority:2;index;size:64" json:"runId"`
	NodeID    string `gorm:"index:idx_gsl_instance,priority:2;index:idx_gsl_kind_instance,priority:3;size:128" json:"nodeId"`
	Iteration int    `gorm:"index:idx_gsl_instance,priority:3;index:idx_gsl_kind_instance,priority:4" json:"iteration"`
	GateID    *uint  `gorm:"index" json:"gateId,omitempty"`
	CreatedBy string `gorm:"size:128" json:"createdBy"`
	TTLTier   string `gorm:"size:8" json:"ttlTier"`
	// PermissionPreset is the link-level capability preset (full|react_only).
	PermissionPreset string     `gorm:"size:32;not null" json:"permissionPreset"`
	ExpiresAt        time.Time  `gorm:"index" json:"expiresAt"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	UsedAt           *time.Time `json:"usedAt,omitempty"`
	UsedAction       string     `gorm:"size:64" json:"usedAction,omitempty"`
	// UsedLane is the visitor lane whose confirm/reject consumed the link.
	UsedLane  string    `gorm:"size:16" json:"usedLane,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// GateShareVisitorConversation is one share-link visitor's own dialogue. Lane
// is a hash of (link, browser visitor id), never the raw id. Messages starts
// as a copy of the node's dialogue at the visitor's first message.
type GateShareVisitorConversation struct {
	ID     uint   `gorm:"primaryKey" json:"-"`
	LinkID string `gorm:"uniqueIndex:idx_gsvc_link_lane,priority:1;size:40;not null" json:"-"`
	Lane   string `gorm:"uniqueIndex:idx_gsvc_link_lane,priority:2;size:16;not null" json:"-"`
	RunID  string `gorm:"index;size:64" json:"-"`
	NodeID string `gorm:"size:128" json:"-"`
	// ChatID is the sandbox bridge chat currently serving the lane ("" when closed).
	ChatID string `gorm:"size:64" json:"-"`
	// Messages opens with a snapshot of the node's own dialogue taken when the
	// visitor first spoke (HistoryLen turns), followed by the visitor's turns.
	Messages     []ReactMessage `gorm:"serializer:json" json:"turns"`
	HistoryLen   int            `json:"-"`
	LastActiveAt time.Time      `json:"-"`
	CreatedAt    time.Time      `json:"-"`
}

// Turns returns the transcript as a non-nil slice so JSON clients always get [].
func (c GateShareVisitorConversation) Turns() []ReactMessage {
	if c.Messages == nil {
		return []ReactMessage{}
	}
	return c.Messages
}

// Share-link permission presets (product: default full).
const (
	SharePermissionFull      = "full"
	SharePermissionReactOnly = "react_only"
)

// Share-link TTL tiers (product: default 24h).
const (
	ShareTTLTier1h  = "1h"
	ShareTTLTier8h  = "8h"
	ShareTTLTier24h = "24h"
	ShareTTLTier72h = "72h"
	ShareTTLTier7d  = "7d"
)

// Share-link product states (no plaintext token).
const (
	ShareLinkStateNone    = "none"
	ShareLinkStateActive  = "active"
	ShareLinkStateUsed    = "used"
	ShareLinkStateRevoked = "revoked"
	ShareLinkStateExpired = "expired"
)
