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
// for its own origin; the answer comes from Grasp over the run's MCP
// credentials instead.
const EmbedOriginPath = "/__grasp/embed-origin"

const embedLookupTimeout = 5 * time.Second

// EmbedLookup resolves drawer tickets against Grasp. A zero value (no run
// credentials in the sandbox) answers 404, and the script runs without a
// drawer.
type EmbedLookup struct {
	// RunURL is GRASP_ARTIFACT_URL: <grasp>/mcp/runs/<runId>.
	RunURL string
	Token  string
	Client *http.Client
}

type embedOrigin struct {
	Origin string `json:"origin"`
	RunID  string `json:"runId"`
	NodeID string `json:"nodeId"`
}

func (l EmbedLookup) configured() bool {
	return strings.TrimSpace(l.RunURL) != "" && strings.TrimSpace(l.Token) != ""
}

func (l EmbedLookup) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	node := strings.TrimSpace(r.URL.Query().Get("node"))
	if !l.configured() || ticket == "" || node == "" {
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
	q := url.Values{"ticket": {ticket}, "nodeId": {node}}
	target := strings.TrimRight(strings.TrimSpace(l.RunURL), "/") + "/embed-origin?" + q.Encode()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		return embedOrigin{}, false
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(l.Token))
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: embedLookupTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return embedOrigin{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return embedOrigin{}, false
	}
	var got embedOrigin
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&got) != nil {
		return embedOrigin{}, false
	}
	if got.NodeID != node || !validOrigin(got.Origin) {
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
