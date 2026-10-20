// Package channels is a platform-agnostic abstraction for external IM channels
// (QQ today; Slack/Discord/Feishu later). Concrete transports implement the
// Adapter interface and normalize their protocol into InboundMessage /
// OutboundMessage.
//
// Reply/Work equivalent orchestration (验收口径，不强制物理双容器):
//
//   - Manager = Reply：唯一对 QQ 的发言出口（即时 ACK、排队 ACK、按需进度、
//     终态、定时推送队列与优先级）。
//   - ChannelBridge / PmTurnRunner / cron 沙箱 = Work：执行用户回合与定时任务，
//     只产出内部 ProgressEvent / TurnFinalReport / CronDelivery，不得旁路
//     adapter.Send 抢发 IM。ACK 在派发 Work 之前由 Manager 发出。
package channels

import (
	"context"
	"errors"
	"time"
)

// Scene classifies a conversation surface. Adapters map their native scopes
// onto these so the bridge and thread naming stay platform-neutral.
type Scene string

const (
	// SceneC2C is a 1:1 private chat.
	SceneC2C Scene = "c2c"
	// SceneGroup is a group chat.
	SceneGroup Scene = "group"
	// SceneGuild is a guild/server channel (QQ 频道).
	SceneGuild Scene = "guild"
)

// Image is a normalized file/image attachment. Inbound attachments carry raw
// bytes (downloaded by the adapter); outbound images are referenced by URL.
// Filename is the original name when the channel provides one; empty means
// callers should use a distinguishable fallback before PromptImage.Name.
type Image struct {
	Data     []byte
	MimeType string
	URL      string
	Filename string
}

// InboundMessage is a normalized user message received from a channel.
type InboundMessage struct {
	Scene          Scene
	ConversationID string // openid / group_openid / channel_id
	UserID         string // author id (openid/union), for display/attribution
	Text           string
	Images         []Image
	MessageID      string // platform message id (passive reply + dedup)
	Timestamp      time.Time
	Raw            map[string]any
	// ChannelHint is an optional PM-only system note (e.g. QQ image download
	// failure). Persisted as role=system source=channel; never sent back to IM.
	ChannelHint string
}

// OutboundMessage is a normalized reply/push to a channel conversation.
type OutboundMessage struct {
	Scene            Scene
	ConversationID   string
	ReplyToMessageID string // passive reply id (empty → active push)
	Text             string
	ImageURLs        []string // shareable image URLs to attach
}

// InboundHandler is invoked by an adapter for each received message.
type InboundHandler func(ctx context.Context, in InboundMessage)

// Connection runtime states exposed on ChannelConfigDTO (process-local).
const (
	ConnStateConnected    = "connected"
	ConnStateAuthFailed   = "auth_failed"
	ConnStateDisconnected = "disconnected"
)

// ErrAdapterAuth marks a credential / tenant_access_token failure.
var ErrAdapterAuth = errors.New("channel adapter authentication failed")

// StatefulAdapter is an optional Adapter that reports long-connection lifecycle.
type StatefulAdapter interface {
	SetStateHandler(fn func(state, detail string))
}

// Adapter is a transport for one external channel account. Implementations are
// created per ChannelConfig and are responsible only for receiving/sending;
// PM orchestration lives in ChannelBridge.
type Adapter interface {
	// Type returns the channel type (e.g. "qq").
	Type() string
	// Start connects and begins delivering inbound messages to onInbound until
	// the ctx is cancelled or Stop is called. It should return once the receive
	// loop is running (non-blocking) or on a fatal startup error.
	Start(ctx context.Context, onInbound InboundHandler) error
	// Send delivers an outbound message.
	Send(ctx context.Context, out OutboundMessage) error
	// Stop tears down the connection and releases resources.
	Stop() error
}

// AdapterConfig is the resolved, decrypted configuration handed to a factory.
type AdapterConfig struct {
	ID        string
	Type      string
	Name      string
	AppID     string
	AppSecret string // decrypted
	Config    map[string]any
	// HasSpoken reports whether the conversation has inbound channel history.
	// Used by wecom proactive push to fail locally with 「未发言」.
	HasSpoken func(scene Scene, conversationID string) bool
}

// AdapterFactory builds an Adapter for a resolved config. Registered in the
// Manager (from main.go) so the channels package never imports concrete
// adapters, keeping the dependency graph acyclic.
type AdapterFactory func(cfg AdapterConfig) (Adapter, error)

// StrOpt reads a string option from an adapter Config map.
func StrOpt(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}

// BoolOpt reads a bool option from an adapter Config map.
func BoolOpt(cfg map[string]any, key string) bool {
	if cfg == nil {
		return false
	}
	if v, ok := cfg[key].(bool); ok {
		return v
	}
	return false
}

// SessionCaps are per-channel write gates for platform MCP tools on external
// Channel turns. Missing keys default to false (compatible with existing
// channels that never set them).
type SessionCaps struct {
	AllowMemoryWrite    bool
	AllowSchedulerWrite bool
}

// SessionCapsFromConfig parses allowMemoryWrite / allowSchedulerWrite from a
// ChannelConfig.Config JSON map. BoolOpt defaults missing keys to false.
func SessionCapsFromConfig(cfg map[string]any) SessionCaps {
	return SessionCaps{
		AllowMemoryWrite:    BoolOpt(cfg, "allowMemoryWrite"),
		AllowSchedulerWrite: BoolOpt(cfg, "allowSchedulerWrite"),
	}
}
