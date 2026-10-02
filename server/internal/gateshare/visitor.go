package gateshare

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// MaxVisitorLanesPerLink caps concurrent visitor conversations on one link
// (the sandbox bridge allows 8 chats including its default one).
const MaxVisitorLanesPerLink = 6

// ValidVisitorID reports whether id is the 32-hex browser visitor id the
// public workbench keeps in localStorage.
func ValidVisitorID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// VisitorLane derives the lane for one browser on one link. Lanes never
// carry the raw visitor id, and the same browser gets unrelated lanes on
// different links. Returns "" for an invalid id.
func VisitorLane(linkID, visitorID string) string {
	visitorID = strings.ToLower(strings.TrimSpace(visitorID))
	if strings.TrimSpace(linkID) == "" || !ValidVisitorID(visitorID) {
		return ""
	}
	sum := sha256.Sum256([]byte(linkID + "|" + visitorID))
	return "v:" + hex.EncodeToString(sum[:])[:12]
}
