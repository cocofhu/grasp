package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
)

// Reserved names for the feedback ledger products. Defined in mcp alongside the
// other product-name constants and re-exported here so callers on this side do
// not need the mcp import.
const (
	// FeedbackIndexArtifactName is the run-level index every round links back
	// to; it is the single entry point for both the Agent and the UI.
	FeedbackIndexArtifactName = mcp.FeedbackIndexArtifactName
	// FeedbackArtifactPrefix marks per-round feedback products. Callers match
	// on it to exclude the ledger from deliverable capture, share links and
	// artifact lists.
	FeedbackArtifactPrefix = mcp.FeedbackArtifactPrefix
)

// feedbackSlugMax caps the node-id segment so a long id cannot push the
// artifact name past what UIs and citation shapes handle comfortably.
const feedbackSlugMax = 40

// IsFeedbackArtifactName reports whether name is part of the feedback ledger
// (a per-round product or the index).
func IsFeedbackArtifactName(name string) bool { return mcp.IsFeedbackArtifactName(name) }

// FeedbackArtifactName builds a feedback product name. ReAct feedback
// (review/clarify) is a current-state document, so every round of one
// node+iteration intentionally shares the same name and upserts its cumulative
// conclusion. Gate and preview feedback use per-round names: they are
// discrete submissions rather than a dialogue.
//
// The result always satisfies the citation name shape
// ^[a-z0-9][a-z0-9._-]*\.[a-z0-9]{1,16}$.
func FeedbackArtifactName(kind, nodeID string, iteration, round int) string {
	if iteration < 1 {
		iteration = 1
	}
	if round < 1 {
		round = 1
	}
	kind = feedbackKindSlug(kind)
	if kind == "review" || kind == "clarify" {
		return fmt.Sprintf("%s%s.%s.i%d.json",
			FeedbackArtifactPrefix, kind, feedbackNodeSlug(nodeID), iteration)
	}
	return fmt.Sprintf("%s%s.%s.i%dr%d.json",
		FeedbackArtifactPrefix, kind, feedbackNodeSlug(nodeID), iteration, round)
}

// feedbackKindSlug keeps the kind segment inside the known set so a malformed
// caller cannot inject separators into the name.
func feedbackKindSlug(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "clarify", "review", "gate", "preview":
		return strings.TrimSpace(strings.ToLower(kind))
	default:
		return "other"
	}
}

// feedbackNodeSlug renders a node id as a name-safe segment.
//
// Any lossy transform (case folding, character replacement, truncation) appends
// a digest of the original id, so two different node ids can never slug to the
// same segment — without that, "Foo" and "foo", or "a/b" and "a-b", would share
// one artifact and silently overwrite each other's rounds.
func feedbackNodeSlug(nodeID string) string {
	raw := strings.TrimSpace(nodeID)
	var b strings.Builder
	mangled := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			mangled = true
		default:
			b.WriteByte('-')
			mangled = true
		}
	}
	slug := collapseDashes(b.String())
	if len(slug) > feedbackSlugMax {
		slug = strings.Trim(slug[:feedbackSlugMax], "-")
		mangled = true
	}
	if slug == "" {
		slug = "node"
		mangled = true
	}
	if mangled {
		slug += "-" + shortDigest(raw)
	}
	return slug
}

func collapseDashes(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if r == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "-")
}

func shortDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:6]
}
