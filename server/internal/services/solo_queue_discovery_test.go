package services

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func queueStoppedDiscoveryFixture(t *testing.T, solo *SoloService, db *gorm.DB, id string, sandboxID uint) {
	t.Helper()
	row := models.SoloSession{ID: id, UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStopped, SandboxID: sandboxID}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	intent, err := solo.durableSoloIntent(&soloTurn{ID: id + "-turn", ActorID: "alice", ClientMessageID: id + "-client", Fingerprint: strings.Repeat("a", 64), Text: "accepted before explicit stop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Admit(context.Background(), soloQueueScope(id), intent, 0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSoloQueueDiscoveryBacksOffStoppedEnvironmentsAndSeparatesMaintenance(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	solo.SetChatterForTest(newRestoreChat(false))
	queueStoppedDiscoveryFixture(t, solo, db, "stopped", 0)
	solo.restoreEnabled = true
	var probes, legacyScans, noticeJoins atomic.Int32
	if err := db.Callback().Query().After("gorm:query").Register("test:discovery_queries", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if tx.Statement.Table == "solo_sessions" {
			probes.Add(1)
		}
		if strings.Contains(sql, "chat_pending_turns") {
			legacyScans.Add(1)
		}
		if strings.Contains(sql, "JOIN chat_queues q") {
			noticeJoins.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for second := range 121 {
		solo.pollSoloQueues(now.Add(time.Duration(second)*time.Second), false)
	}
	if count := probes.Load(); count < 6 || count > 9 {
		t.Fatalf("stopped environment was polled every second or never retried: %d reads", count)
	}
	if legacyScans.Load() != 0 || noticeJoins.Load() != 0 {
		t.Fatalf("fast discovery repeated slow maintenance: legacy=%d notices=%d", legacyScans.Load(), noticeJoins.Load())
	}
	if solo.lookup("stopped") != nil {
		t.Fatal("passive discovery woke stopped compute")
	}
	solo.pollSoloQueues(now.Add(122*time.Second), true)
	if legacyScans.Load() != 0 || noticeJoins.Load() != 1 {
		t.Fatalf("maintenance scanned obsolete sources or lost durable notices: legacy=%d notices=%d", legacyScans.Load(), noticeJoins.Load())
	}
	if err := solo.durable.Remove(context.Background(), soloQueueScope("stopped"), "stopped-turn"); err != nil {
		t.Fatal(err)
	}
	solo.pollSoloQueues(now.Add(123*time.Second), false)
	if len(solo.queueProbes) != 0 {
		t.Fatal("discovery retained unbounded backoff entries for settled queues")
	}
}

func TestSoloQueueEnvironmentRecoveryClearsOnlyItsOwnDiscoveryBackoff(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	solo.SetChatterForTest(newRestoreChat(false))
	queueStoppedDiscoveryFixture(t, solo, db, "target", 42)
	queueStoppedDiscoveryFixture(t, solo, db, "other", 43)
	solo.restoreEnabled = true
	future := time.Now().Add(time.Hour)
	solo.queueProbes = map[string]soloQueueProbe{
		"target": {Next: future, Delay: soloQueueMaxProbeDelay},
		"other":  {Next: future, Delay: soloQueueMaxProbeDelay},
	}
	solo.retryDeferredRestores(42)
	if probe := solo.queueProbes["target"]; probe.Delay != 2*soloQueueDiscoveryInterval || !probe.Next.Before(future) {
		t.Fatalf("explicit environment recovery waited for stale backoff: %+v", probe)
	}
	if probe := solo.queueProbes["other"]; !probe.Next.Equal(future) || probe.Delay != soloQueueMaxProbeDelay {
		t.Fatalf("one environment bypassed another's passive backoff: %+v", probe)
	}
}
