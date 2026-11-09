package chatsession

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestJournalWriterLowVolumeWaitsButBarriersAndThresholdsCommitImmediately(t *testing.T) {
	for _, trigger := range []string{"flush", "close", "size", "count", "timer"} {
		t.Run(trigger, func(t *testing.T) {
			committed := make(chan int, 4)
			writer := NewJournalWriter(t.Context(), func(_ context.Context, entries []JournalEntry) (JournalRange, error) {
				committed <- len(entries)
				return JournalRange{First: 0, Last: uint64(len(entries) - 1), Count: len(entries)}, nil
			}, nil)
			t.Cleanup(func() { _ = writer.Close(context.Background()) })
			entry := JournalEntry{Type: FrameSession, Event: "output", Payload: map[string]any{"text": "small"}}
			if trigger == "size" {
				entry.Payload["text"] = strings.Repeat("x", journalWriterTarget)
			}
			if err := writer.Add(t.Context(), entry); err != nil {
				t.Fatal(err)
			}
			if trigger == "size" || trigger == "count" {
				if trigger == "count" {
					for range JournalBatchEvents - 1 {
						if err := writer.Add(t.Context(), entry); err != nil {
							t.Fatal(err)
						}
					}
				}
			} else {
				select {
				case <-committed:
					t.Fatal("low-volume output still caused 100ms storage writes")
				case <-time.After(250 * time.Millisecond):
				}
			}
			deadline := journalWriterFlushInterval / 2
			if trigger == "timer" {
				deadline = 2 * journalWriterFlushInterval
			}
			ctx, cancel := context.WithTimeout(t.Context(), deadline)
			defer cancel()
			if trigger == "flush" {
				if err := writer.Flush(ctx); err != nil {
					t.Fatal("Flush waited for periodic timer", err)
				}
			} else if trigger == "close" {
				if err := writer.Close(ctx); err != nil {
					t.Fatal("Close waited for periodic timer", err)
				}
			}
			select {
			case count := <-committed:
				if count != 1 && (trigger != "count" || count != JournalBatchEvents) {
					t.Fatal("incorrect frozen batch size", count)
				}
			case <-ctx.Done():
				t.Fatalf("%s failed to commit within its own trigger deadline", trigger)
			}
		})
	}
}
