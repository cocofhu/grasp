package models

import (
	"encoding/json"
	"strconv"
	"time"

	"sandbox-gateway/internal/logging"
)

func itoa(n int) string { return strconv.Itoa(n) }

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// Sandbox lifecycle states.
const (
	StatusPulling  = "pulling"  // image missing locally; docker pull in progress
	StatusCreating = "creating" // record persisted, container/pod being provisioned
	StatusRunning  = "running"  // ready and endpoints backfilled
	StatusStopped  = "stopped"  // stopped but retained (docker stop / scale 0)
	StatusError    = "error"    // provisioning or reconcile failed
)

// Sandbox is the persisted metadata for one sandbox instance. Endpoints holds
// the address for each exposed container port. Legacy clients dial directly;
// the authenticated gateway transport resolves live driver addresses itself.
type Sandbox struct {
	ID        string `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"index" json:"name"` // driver-native resource name (container/deployment)
	Status    string `gorm:"index" json:"status"`
	Image     string `json:"image"`
	Namespace string `json:"namespace,omitempty"` // k8s only
	Error     string `json:"error,omitempty"`

	// Per-sandbox resource limits (0 means legacy/default at create time).
	CPUCores float64 `json:"cpuCores"`
	MemoryMB int64   `json:"memoryMB"`
	DiskGi   int64   `json:"diskGi"`

	// EnvJSON / EndpointsJSON / LabelsJSON persist maps as JSON columns.
	EnvJSON       string `gorm:"column:env;type:text" json:"-"`
	EndpointsJSON string `gorm:"column:endpoints;type:text" json:"-"`
	LabelsJSON    string `gorm:"column:labels;type:text" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Endpoints decodes the port->address map (e.g. {8765:"10.0.0.5:8765"}).
func (s *Sandbox) Endpoints() map[int]string {
	return decodeIntMap(s.EndpointsJSON)
}

// SetEndpoints encodes the port->address map.
func (s *Sandbox) SetEndpoints(m map[int]string) {
	s.EndpointsJSON = encodeIntMap(m)
}

// Env decodes the injected environment variables.
func (s *Sandbox) Env() map[string]string {
	return decodeStrMap(s.EnvJSON)
}

// SetEnv encodes the injected environment variables.
func (s *Sandbox) SetEnv(m map[string]string) {
	s.EnvJSON = encodeStrMap(m)
}

// Labels decodes caller-supplied labels.
func (s *Sandbox) Labels() map[string]string {
	return decodeStrMap(s.LabelsJSON)
}

// SetLabels encodes caller-supplied labels.
func (s *Sandbox) SetLabels(m map[string]string) {
	s.LabelsJSON = encodeStrMap(m)
}

func encodeStrMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	logging.WarnErr(err, "encode env/labels map", nil)
	return string(b)
}

func decodeStrMap(s string) map[string]string {
	m := map[string]string{}
	if s == "" {
		return m
	}
	logging.WarnErr(json.Unmarshal([]byte(s), &m), "decode env/labels map", map[string]any{"bytes": len(s)})
	return m
}

func encodeIntMap(m map[int]string) string {
	if len(m) == 0 {
		return ""
	}
	// JSON object keys are strings; store as {"8765":"addr"}.
	tmp := make(map[string]string, len(m))
	for k, v := range m {
		tmp[itoa(k)] = v
	}
	b, err := json.Marshal(tmp)
	logging.WarnErr(err, "encode endpoints map", nil)
	return string(b)
}

func decodeIntMap(s string) map[int]string {
	out := map[int]string{}
	if s == "" {
		return out
	}
	tmp := map[string]string{}
	if err := json.Unmarshal([]byte(s), &tmp); err != nil {
		logging.WarnErr(err, "decode endpoints map", map[string]any{"bytes": len(s)})
		return out
	}
	for k, v := range tmp {
		if n, ok := atoi(k); ok {
			out[n] = v
		}
	}
	return out
}
