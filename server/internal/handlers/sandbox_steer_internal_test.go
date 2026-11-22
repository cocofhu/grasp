package handlers

import (
	"context"
	"slices"
	"testing"
)

func TestAClosedChatSocketDropsItsWaitingSteers(t *testing.T) {
	ctx, closeSocket := context.WithCancel(context.Background())
	steers := make(chan string, maxPendingSteers)
	steers <- "a"
	steers <- "b"
	steers <- "c"
	close(steers)
	var delivered []string
	runSteers(ctx, steers, func(itemID string) {
		delivered = append(delivered, itemID)
		closeSocket()
	})
	if !slices.Equal(delivered, []string{"a"}) {
		t.Fatalf("delivered = %v, want only the steer in progress", delivered)
	}
}
