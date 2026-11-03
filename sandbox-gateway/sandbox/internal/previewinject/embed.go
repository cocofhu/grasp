package previewinject

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EmbedOriginPath lets the pick script learn which Grasp origin a chat-drawer
// ticket belongs to. The ticket arrives in the page URL, so it cannot vouch
// for its own origin; the answer comes from the Grasp server configured by
// the sandbox bootstrap.
const EmbedOriginPath = "/__grasp/embed-origin"

const embedLookupTimeout = 5 * time.Second

// EmbedLookup resolves drawer tickets against Grasp. A zero value answers 404.
type EmbedLookup struct {
	// RunURL is GRASP_ARTIFACT_URL: <grasp>/mcp/runs/<runId>.
	RunURL string
	Token  string
	// Solo sandboxes can have artifact MCP configured only inside the agent.
	// The runtime download URL still identifies the Grasp server.
	RuntimeURL string
	RunID      string
	Client     *http.Client
}

type embedOrigin struct {
	Origin string `json:"origin"`
	RunID  string `json:"runId"`
	NodeID string `json:"nodeId"`
	Kind   string `json:"kind,omitempty"`
}

func (l EmbedLookup) endpoint() (target, token string) {
	if run, token := strings.TrimSpace(l.RunURL), strings.TrimSpace(l.Token); run != "" && token != "" {
		return strings.TrimRight(run, "/") + "/embed-origin", token
	}
	runtime, err := url.Parse(strings.TrimSpace(l.RuntimeURL))
	runID := strings.TrimSpace(l.RunID)
	if err != nil || runtime.Host == "" || (runtime.Scheme != "http" && runtime.Scheme != "https") || runID == "" {
		return "", ""
	}
	return runtime.Scheme + "://" + runtime.Host + "/embed-api/origin/" + url.PathEscape(runID), ""
}

func (l EmbedLookup) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	node := strings.TrimSpace(r.URL.Query().Get("node"))
	if ticket == "" || node == "" {
		http.NotFound(w, r)
		return
	}
	got, ok := l.lookup(r, ticket, node)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(got)
}

func (l EmbedLookup) lookup(r *http.Request, ticket, node string) (embedOrigin, bool) {
	target, token := l.endpoint()
	if target == "" {
		return embedOrigin{}, false
	}
	q := url.Values{"ticket": {ticket}, "nodeId": {node}}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target+"?"+q.Encode(), nil)
	if err != nil {
		return embedOrigin{}, false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: embedLookupTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return embedOrigin{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return embedOrigin{}, false
	}
	var got embedOrigin
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&got) != nil {
		return embedOrigin{}, false
	}
	if got.NodeID != node || (l.RunID != "" && got.RunID != l.RunID) || !validOrigin(got.Origin) {
		return embedOrigin{}, false
	}
	return got, true
}

func validOrigin(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}
