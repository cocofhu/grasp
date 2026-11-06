package tokenledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const recorderName = "grasp-token-ledger"

// Recorder keeps a write-ahead journal on persistent storage, independent of
// database availability. Files contain accounting metadata only, never prompts
// or credentials. SQL receipts make replay safe after an uncertain commit.
type Recorder struct {
	db   *gorm.DB
	dir  string
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
	once sync.Once
	// Preserve reports in memory as a last resort when both stores fail. Such
	// a failure is logged at Error; it cannot be made durable without storage.
	unsaved map[string]pendingReport
}

func (r *Recorder) Name() string                 { return recorderName }
func (r *Recorder) Initialize(db *gorm.DB) error { r.db = db; return nil }

// Start installs the recorder before workers begin, replays the journal and
// keeps retrying pending reports. dir must be on persistent storage.
func Start(db *gorm.DB, dir string) (*Recorder, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("token ledger journal directory required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r := &Recorder{dir: dir, stop: make(chan struct{}), done: make(chan struct{}), unsaved: map[string]pendingReport{}}
	if err := db.Use(r); err != nil {
		return nil, err
	}
	r.Flush()
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-ticker.C:
				r.Flush()
			}
		}
	}()
	return r, nil
}

func (r *Recorder) Close() { r.once.Do(func() { close(r.stop) }); <-r.done }

func (r *Recorder) record(report pendingReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.dir, report.Receipt.ID+".json")
	if previous, ok := r.unsaved[report.Receipt.ID]; ok && !replaceReport(previous.Receipt, report.Receipt) {
		report = previous
	}
	// A late provisional replay must not replace a completed report that is
	// waiting for SQL recovery. Apply the same ordering as SQL receipts.
	if raw, err := os.ReadFile(path); err == nil {
		var previous pendingReport
		if json.Unmarshal(raw, &previous) == nil && !replaceReport(previous.Receipt, report.Receipt) {
			report = previous
		}
	}
	journalErr := r.journal(path, report)
	if err := r.commit(report); err != nil {
		if journalErr != nil {
			r.unsaved[report.Receipt.ID] = report
			log.Error().Err(errors.Join(err, journalErr)).Str("report_id", report.Receipt.ID).
				Msg("token accounting storage unavailable; report retained in memory")
		} else {
			log.Warn().Err(err).Str("report_id", report.Receipt.ID).Msg("token report journaled; database replay pending")
		}
		return
	}
	delete(r.unsaved, report.Receipt.ID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Msg("remove committed token journal")
	}
}

func (r *Recorder) commit(report pendingReport) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return commitReport(r.db.WithContext(ctx), report)
}

func (r *Recorder) journal(path string, report pendingReport) error {
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(r.dir, ".report-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(r.dir)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

// Flush retries journaled reports. Malformed files stay in place and are
// reported, allowing recovery instead of silently deleting accounting data.
func (r *Recorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, report := range r.unsaved {
		if err := r.journal(filepath.Join(r.dir, id+".json"), report); err == nil {
			delete(r.unsaved, id)
		}
	}
	files, err := os.ReadDir(r.dir)
	if err != nil {
		log.Error().Err(err).Msg("read token ledger journal")
		return
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") && !strings.HasPrefix(file.Name(), ".report-") {
			continue
		}
		path := filepath.Join(r.dir, file.Name())
		raw, err := os.ReadFile(path)
		var report pendingReport
		if err == nil {
			err = json.Unmarshal(raw, &report)
		}
		if err == nil && (len(report.Receipt.ID) != 64 || !strings.HasPrefix(file.Name(), ".report-") && file.Name() != report.Receipt.ID+".json") {
			err = fmt.Errorf("invalid token journal identity: %s", file.Name())
		}
		if err != nil {
			log.Error().Err(err).Msg("read token report; journal retained")
			continue
		}
		if err := r.commit(report); err != nil {
			return
		}
		if err := os.Remove(path); err != nil {
			log.Warn().Err(err).Msg("remove token journal")
		}
	}
}

// Pending is the number of reports not yet committed, including storage errors.
func Pending(db *gorm.DB) int {
	r, ok := db.Config.Plugins[recorderName].(*Recorder)
	if !ok {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	files, err := os.ReadDir(r.dir)
	if err != nil {
		return len(r.unsaved) + 1
	}
	n := len(r.unsaved)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") || strings.HasPrefix(f.Name(), ".report-") {
			n++
		}
	}
	return n
}
