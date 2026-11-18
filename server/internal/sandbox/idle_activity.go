package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// RuntimeActivity reports whether the guest runtime has an executing model
// turn or a queued prompt. Processes, terminals and open clients are not work.
// Failure is deliberately unknown (an error), never evidence of inactivity.
func (s *Sandbox) RuntimeActivity(ctx context.Context) (bool, error) {
	cmd, err := newSafeCmd("python3", "-")
	if err != nil {
		return false, err
	}
	out, err := s.creds().runInput(ctx, 10*time.Second, cmd, strings.NewReader(runtimeActivityScript))
	if err != nil {
		return false, err
	}
	var activity struct {
		Busy *bool `json:"busy"`
	}
	if err := json.Unmarshal(out, &activity); err != nil {
		return false, err
	}
	if activity.Busy == nil {
		return false, errors.New("sandbox activity probe omitted busy state")
	}
	return *activity.Busy, nil
}

const runtimeActivityScript = `
import json, urllib.request
with urllib.request.urlopen('http://127.0.0.1:8765/api/runtime/activity', timeout=5) as r:
    activity = json.load(r)
if not isinstance(activity.get('busy'), bool):
    raise RuntimeError('runtime did not report activity')
print(json.dumps({'busy': activity['busy']}))
`
