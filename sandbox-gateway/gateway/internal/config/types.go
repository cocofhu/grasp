package config

// Config describes one gateway deployment. Exactly one backend section is
// present, matching Driver. Backend-specific defaults never cross that boundary.
type Config struct {
	Server     ServerConfig      `yaml:"server"`
	Driver     string            `yaml:"driver"`
	Database   DBConfig          `yaml:"database"`
	Ports      PortsConfig       `yaml:"ports"`
	Docker     *DockerConfig     `yaml:"docker,omitempty"`
	Kubernetes *KubernetesConfig `yaml:"kubernetes,omitempty"`
	E2B        *E2BConfig        `yaml:"e2b,omitempty"`
	Auth       AuthConfig        `yaml:"auth"`
	OrphanGC   OrphanGCConfig    `yaml:"orphanGC"`
}

type ServerConfig struct {
	Listen                 string `yaml:"listen"`
	FinalizeTimeoutSeconds int    `yaml:"finalizeTimeoutSeconds"`
}

type OrphanGCConfig struct {
	Enabled         bool `yaml:"enabled"`
	IntervalSeconds int  `yaml:"intervalSeconds"`
	MinAgeSeconds   int  `yaml:"minAgeSeconds"`
}

type AuthConfig struct {
	APIKeys []string `yaml:"apiKeys"`
}

// ProfileConfig is the environment advertised by the gateway discovery API.
type ProfileConfig struct {
	Image string `yaml:"image" json:"image"`
}

type DBConfig struct {
	// Driver is "sqlite" (local testing) or "mysql" (production). Default sqlite.
	Driver string `yaml:"driver"`
	// Path is the SQLite file path (driver=sqlite).
	Path string `yaml:"path"`
	// MySQL fields (driver=mysql). Prefer DSN when set; otherwise build from parts.
	DSN      string `yaml:"dsn"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`   // database name
	Params   string `yaml:"params"` // DSN query string; default charset=utf8mb4&parseTime=True&loc=Local
}

type PortsConfig struct {
	Session    int   `yaml:"session"`    // ACP bridge / WSP session, default 8765
	CodeServer int   `yaml:"codeServer"` // code-server, default 8744
	SSH        int   `yaml:"ssh"`        // sshd, default 22
	CDP        int   `yaml:"cdp"`        // Chromium CDP, default 9222 (internal-only)
	NoVNC      int   `yaml:"novnc"`      // noVNC websockify, default 6080 (internal-only)
	App        []int `yaml:"app"`        // extra user application ports to expose
}

func collectPorts(vals ...int) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, v := range vals {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Public returns ports that may be published to untrusted networks
// (Docker -p / K8s LoadBalancer): session, IDE, SSH, and extra app ports.
// CDP and noVNC are intentionally omitted — they have no application-layer auth.
func (p PortsConfig) Public() []int {
	vals := []int{p.Session, p.CodeServer, p.SSH}
	vals = append(vals, p.App...)
	return collectPorts(vals...)
}

// Internal returns ports that stay on the container / cluster network only
// (CDP 9222, noVNC 6080). Grasp dials these in-cluster; they must not be
// published to the host or an external LoadBalancer.
func (p PortsConfig) Internal() []int {
	return collectPorts(p.CDP, p.NoVNC)
}

// All returns the external publish set. Historically this included CDP/noVNC;
// those are now Internal-only. Callers that publish to untrusted networks
// MUST use Public()/All(), not Internal().
func (p PortsConfig) All() []int {
	return p.Public()
}

// Listen returns Public ∪ Internal — every port the sandbox process listens on
// (ClusterIP / container ports). Not the external publish set.
func (p PortsConfig) Listen() []int {
	return collectPorts(append(p.Public(), p.Internal()...)...)
}

// ResourceValues are CPU cores, memory MiB and workspace GiB.
type ResourceValues struct {
	CPUCores float64 `yaml:"cpuCores"`
	MemoryMB int64   `yaml:"memoryMB"`
	DiskGi   int64   `yaml:"diskGi"`
}

type ResourceConfig struct {
	Default ResourceValues `yaml:"default"`
	Max     ResourceValues `yaml:"max"`
}

type DockerConfig struct {
	Image      string                   `yaml:"image"`
	Profiles   map[string]ProfileConfig `yaml:"profiles,omitempty"`
	BindIP     string                   `yaml:"bindIP"`
	Network    string                   `yaml:"network"`
	NamePrefix string                   `yaml:"namePrefix"`
	ShmSize    string                   `yaml:"shmSize"`
	Resources  ResourceConfig           `yaml:"resources"`
}

type KubernetesConfig struct {
	Image              string                   `yaml:"image"`
	Profiles           map[string]ProfileConfig `yaml:"profiles,omitempty"`
	InCluster          bool                     `yaml:"inCluster"`
	Kubeconfig         string                   `yaml:"kubeconfig"`
	Namespace          string                   `yaml:"namespace"`
	NamePrefix         string                   `yaml:"namePrefix"`
	StorageClass       string                   `yaml:"storageClass"`
	PVCAnnotations     map[string]string        `yaml:"pvcAnnotations,omitempty"`
	ImagePullSecret    string                   `yaml:"imagePullSecret"`
	ImagePullPolicy    string                   `yaml:"imagePullPolicy"`
	EnableLoadBalancer bool                     `yaml:"enableLoadBalancer"`
	Resources          ResourceConfig           `yaml:"resources"`
	// Zero fixed requests select ratio-based scheduling requests.
	CPURequestCores    float64 `yaml:"cpuRequestCores"`
	MemoryRequestMB    int64   `yaml:"memoryRequestMB"`
	CPURequestRatio    float64 `yaml:"cpuRequestRatio"`
	MemoryRequestRatio float64 `yaml:"memoryRequestRatio"`
}

// E2BConfig has explicit provider addresses and named prepared templates.
// One template is selected automatically; multiple templates require a default.
type E2BConfig struct {
	APIURL               string                       `yaml:"apiURL"`
	SandboxURL           string                       `yaml:"sandboxURL"`
	APIKey               string                       `yaml:"apiKey" json:"-"`
	OwnerID              string                       `yaml:"ownerID"`
	StateDir             string                       `yaml:"stateDir"`
	BootTimeoutSeconds   int                          `yaml:"bootTimeoutSeconds"`
	LeaseSeconds         int                          `yaml:"leaseSeconds"`
	RenewIntervalSeconds int                          `yaml:"renewIntervalSeconds"`
	AutoPause            bool                         `yaml:"autoPause"`
	DefaultTemplate      string                       `yaml:"defaultTemplate,omitempty"`
	Templates            map[string]E2BTemplateConfig `yaml:"templates"`
}

type E2BTemplateConfig struct {
	TemplateID string  `yaml:"templateID"`
	CPUCores   float64 `yaml:"cpuCores"`
	MemoryMB   int64   `yaml:"memoryMB"`
	// Minimum initially available workspace; not a resizable disk quota.
	DiskGi int64 `yaml:"diskGi"`
}
