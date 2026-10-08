package driver

import (
	"context"
	"errors"
	"time"
)

// ErrLogsUnsupported is returned by drivers that do not implement container
// log retrieval. Handlers map it to HTTP 501. Docker and kubernetes implement Logs;
// other/future drivers may still return this error.
var ErrLogsUnsupported = errors.New("sandbox logs not supported by this driver")

// Sandbox lifecycle statuses reported by drivers.
type Status string

const (
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusNotFound Status = "not_found"
	StatusPending  Status = "pending"
	StatusError    Status = "error"
)

// ConfigInject describes optional rules/skills/mcp configuration to seed into
// the sandbox at creation time. Exactly one of BundleURL / HostPath is used per
// driver capability; the driver translates it to the image's SANDBOX_INJECT
// contract or a native bind-mount. ConfigRoot is the in-container destination
// (empty => image default, e.g. /root/.cursor).
type ConfigInject struct {
	ConfigRoot string
	HostPath   string // local dir to bind-mount (docker, same host)
	BundleURL  string // tar.gz / presigned URL translated to SANDBOX_INJECT
	Headers    string // SANDBOX_INJECT_HEADERS for authenticated BundleURL fetch
}

// Resources are per-sandbox compute/storage limits (aligned with remote-dev).
// Zero fields mean "driver/config default". DiskGi applies to the k8s PVC;
// docker ignores DiskGi (host filesystem).
type Resources struct {
	CPUCores float64 // CPU limit in cores (e.g. 2, 0.5)
	MemoryMB int64   // memory limit in MiB
	DiskGi   int64   // data PVC size in GiB (kubernetes)
}

// Spec describes one sandbox to create. The image is a generic agent runner;
// all integration behavior rides through Env (e.g. GIT_REPOS, AGENT_PROVIDER,
// ROOT_PASSWORD, SSH_KEY, ACP_BRIDGE_PASSWORD). The gateway stays kind-agnostic.
type Spec struct {
	ID           string            // gateway-assigned id (also seeds the resource name)
	Image        string            // container image ref
	Env          map[string]string // environment injected into the container
	Ports         []int            // public ports published to host / LoadBalancer (session/ide/ssh/app)
	InternalPorts []int            // cdp/novnc — cluster/container network only, not published externally
	WorkspaceDir string            // WORKSPACE_DIR inside the container
	Mounts       []string          // extra docker "-v host:container[:ro]" specs (docker only)
	Config       *ConfigInject     // optional rules/skills/mcp injection
	Labels       map[string]string // caller-supplied labels/metadata
	Resources    Resources         // CPU / memory / disk limits
}

// Handle is the driver's view of a live sandbox.
type Handle struct {
	ID        string         // gateway id
	Name      string         // driver-native resource name
	Namespace string         // k8s namespace (empty for docker)
	Status    Status         // current lifecycle status
	Endpoints map[int]string // container port -> client-reachable "host:port"
	// CreatedAt is when the underlying workload first appeared (zero if unknown).
	// Used by orphan GC age filtering.
	CreatedAt time.Time
}

// ExitInfo is the most recent termination of a sandbox's main container.
type ExitInfo struct {
	Reason    string    `json:"reason"` // OOMKilled | Error | Evicted | Completed | Exited
	ExitCode  int       `json:"exitCode"`
	OOMKilled bool      `json:"oomKilled"`
	Message   string    `json:"message,omitempty"`
	Restarts  int       `json:"restarts"`
	MemoryMB  int64     `json:"memoryMB,omitempty"` // memory limit in force, 0 if unknown
	At        time.Time `json:"at,omitempty"`
}

// ExitReporter is implemented by drivers that can say why a sandbox's main
// container last exited. LastExit returns (nil, nil) when it never has.
type ExitReporter interface {
	LastExit(ctx context.Context, id string) (*ExitInfo, error)
}

// Driver provisions and exposes sandboxes. It is intentionally thin: lifecycle
// plus endpoint exposure only. Data-plane operations (exec, files, terminal,
// sessions, IDE, preview) are direct client-to-sandbox connections and are NOT
// part of this interface.
//
// A gateway instance uses exactly one Driver, chosen at startup by config
// (docker for local testing, kubernetes for production).
type Driver interface {
	// Name returns the driver identifier ("docker" | "kubernetes").
	Name() string

	// Create provisions a sandbox and returns once the underlying resource is
	// created (not necessarily ready). Endpoints may be partially populated;
	// the service layer finalizes readiness and backfills addresses.
	Create(ctx context.Context, spec Spec) (*Handle, error)

	// Start resumes a stopped sandbox (docker start / scale=1).
	Start(ctx context.Context, id string) error

	// Stop stops a sandbox but retains it for restart (docker stop / scale=0).
	Stop(ctx context.Context, id string) error

	// Destroy removes the sandbox and its resources (best effort).
	Destroy(ctx context.Context, id string) error

	// Reinstall rebuilds the sandbox workload. When preserveData is true, data
	// volumes (k8s PVC / docker anonymous volumes) are kept; otherwise they are
	// deleted before recreate. Shared host-mounted config (e.g. .cursor via
	// ConfigInject.HostPath) is never deleted by this call.
	Reinstall(ctx context.Context, spec Spec, preserveData bool) error

	// Get returns the current handle, refreshing status and endpoints.
	Get(ctx context.Context, id string) (*Handle, error)

	// List returns all sandboxes this driver manages (for reconcile).
	List(ctx context.Context) ([]*Handle, error)

	// Status returns just the lifecycle status of a sandbox.
	Status(ctx context.Context, id string) (Status, error)

	// Endpoints returns the client-reachable address for each exposed port.
	Endpoints(ctx context.Context, id string) (map[int]string, error)

	// Logs returns the sandbox PID1 combined stdout/stderr (non-follow).
	// tail limits the number of lines from the end; values <= 0 use a driver default.
	// Drivers that cannot provide logs return ErrLogsUnsupported.
	Logs(ctx context.Context, id string, tail int) (string, error)
}
