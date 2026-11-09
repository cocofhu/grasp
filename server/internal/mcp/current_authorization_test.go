package mcp

import "testing"

func TestCurrentAuthorizationRejectsCachedAndRestoredTokens(t *testing.T) {
	h := NewHost(nil)
	token := h.RegisterRun("resident")
	allowed, calls := true, 0
	h.SetCurrentAuthorization(func(run string) bool { calls++; return run == "resident" && allowed })
	if h.authorize("resident", "wrong") || calls != 0 {
		t.Fatal("invalid tokens triggered authorization resolution")
	}
	if !h.authorize("resident", token) || calls != 1 {
		t.Fatal("cached token did not verify the current grant")
	}
	allowed = false
	if h.authorize("resident", token) {
		t.Fatal("revoked grant retained cached-token authority")
	}
	h.SetRunTokenSource(func(string) (string, bool, bool) { return token, true, true })
	if h.authorize("resident", token) {
		t.Fatal("persistence fallback bypassed revocation")
	}
	allowed = true
	if !h.authorize("resident", token) {
		t.Fatal("currently authorized persistence fallback could not restore")
	}
	h.SetCurrentAuthorization(nil)
	if !h.authorize("resident", token) {
		t.Fatal("ordinary workflow token contract changed")
	}
}
