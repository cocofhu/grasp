package sandbox

import (
	"testing"
)

func TestBundleStorePutWithTokenShared(t *testing.T) {
	s := NewBundleStore()
	token := "shared-token-xyz"
	id1 := s.PutWithToken([]byte("aaa"), DefaultInjectBundleTTL, token)
	id2 := s.PutWithToken([]byte("bbb"), DefaultInjectBundleTTL, token)
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Fatalf("ids %q %q", id1, id2)
	}
	b1, ok := s.Get(id1, token)
	if !ok || string(b1) != "aaa" {
		t.Fatalf("get1 %v %q", ok, b1)
	}
	b2, ok := s.Get(id2, token)
	if !ok || string(b2) != "bbb" {
		t.Fatalf("get2 %v %q", ok, b2)
	}
}
