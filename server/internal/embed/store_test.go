package embed

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.EmbedTicket{}, &models.EmbedSession{}); err != nil {
		t.Fatal(err)
	}
	return NewStore(db)
}

func sessionClaims() Claims {
	return Claims{
		Kind:        models.EmbedKindSession,
		RunID:       "run-1",
		NodeID:      "grasp1",
		Username:    "admin",
		GraspOrigin: "http://grasp.example/",
	}
}

func TestTicketPeekRedeemOnce(t *testing.T) {
	s := newTestStore(t)
	ticket, exp, err := s.IssueTicket(sessionClaims())
	if err != nil {
		t.Fatal(err)
	}
	if !ValidTicketShape(ticket) || time.Until(exp) > TicketTTL || time.Until(exp) <= 0 {
		t.Fatalf("ticket=%q exp=%v", ticket, exp)
	}
	peek, ok := s.PeekTicket(ticket)
	if !ok || peek.GraspOrigin != "http://grasp.example" || peek.NodeID != "grasp1" {
		t.Fatalf("peek ok=%v %+v", ok, peek)
	}
	if _, ok := s.PeekTicket(ticket); !ok {
		t.Fatal("peek must not consume")
	}
	got, ok := s.RedeemTicket(ticket)
	if !ok || got.Username != "admin" || got.RunID != "run-1" {
		t.Fatalf("redeem ok=%v %+v", ok, got)
	}
	if _, ok := s.RedeemTicket(ticket); ok {
		t.Fatal("ticket redeemed twice")
	}
	if _, ok := s.PeekTicket(ticket); ok {
		t.Fatal("consumed ticket still peekable")
	}
}

func TestTicketExpires(t *testing.T) {
	s := newTestStore(t)
	ticket, _, err := s.IssueTicket(sessionClaims())
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(TicketTTL + time.Second) }
	if _, ok := s.PeekTicket(ticket); ok {
		t.Fatal("expired ticket peekable")
	}
	if _, ok := s.RedeemTicket(ticket); ok {
		t.Fatal("expired ticket redeemed")
	}
}

func TestIssueRejectsIncompleteClaims(t *testing.T) {
	s := newTestStore(t)
	bad := []Claims{
		{Kind: models.EmbedKindSession, RunID: "r", NodeID: "n", GraspOrigin: "http://g"},
		{Kind: models.EmbedKindShare, RunID: "r", NodeID: "n", GraspOrigin: "http://g"},
		{Kind: "other", RunID: "r", NodeID: "n", Username: "u", GraspOrigin: "http://g"},
		{Kind: models.EmbedKindSession, RunID: "r", NodeID: "n", Username: "u"},
	}
	for i, c := range bad {
		if _, _, err := s.IssueTicket(c); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestSessionLookupAndInvalidate(t *testing.T) {
	s := newTestStore(t)
	token, _, err := s.CreateSession(sessionClaims())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, SessionPrefix) || !IsSessionToken(token) {
		t.Fatalf("token shape %q", token)
	}
	c, ok := s.LookupSession(token)
	if !ok || c.Username != "admin" || c.Kind != models.EmbedKindSession {
		t.Fatalf("lookup ok=%v %+v", ok, c)
	}
	if _, ok := s.LookupSession(strings.TrimPrefix(token, SessionPrefix)); ok {
		t.Fatal("unprefixed token accepted")
	}
	s.InvalidateRun("run-1")
	if _, ok := s.LookupSession(token); ok {
		t.Fatal("session survived run invalidation")
	}
}

func TestShareSessionInvalidateByHash(t *testing.T) {
	s := newTestStore(t)
	token, _, err := s.CreateSession(Claims{
		Kind: models.EmbedKindShare, RunID: "run-1", NodeID: "ap", ShareTokenHash: "h1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := s.LookupSession(token); !ok || c.ShareTokenHash != "h1" {
		t.Fatalf("lookup ok=%v %+v", ok, c)
	}
	s.InvalidateShare("h1")
	if _, ok := s.LookupSession(token); ok {
		t.Fatal("share session survived link invalidation")
	}
}

func TestSessionExpires(t *testing.T) {
	s := newTestStore(t)
	token, _, err := s.CreateSession(sessionClaims())
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(SessionTTL + time.Second) }
	if _, ok := s.LookupSession(token); ok {
		t.Fatal("expired session accepted")
	}
}
