package services

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestOpenTerminalPassesSandboxToken(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()
	const token = "terminal-sandbox-token"
	row := &models.Sandbox{Name: "grasp-sb-term-tok", Purpose: "test", Status: "running", Token: token}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	ds.fg.SSHPort = 1
	ds.setStatus(row.Name, "running")

	var mu sync.Mutex
	var seen []string
	restore := sandbox.SetDataPlaneAuthObserver(func(p string) {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	})
	defer restore()

	_, err := s.OpenTerminal(ctx, row.ID)
	if err != nil && (strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "PRIVATE KEY") || strings.Contains(err.Error(), "ROOT_PASSWORD")) {
		t.Fatal("terminal error contains a secret")
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, p := range seen {
		if p == token {
			found = true
		}
	}
	if !found {
		t.Fatal("OpenTerminal did not pass the sandbox token")
	}
}
