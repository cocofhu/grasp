package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound identifies an absent provider sandbox without exposing its body.
var ErrNotFound = errors.New("E2B sandbox not found")

// APIError contains only an operation name and status, never a provider body,
// URL, command, environment variable, or authentication header.
type APIError struct {
	Operation  string
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("E2B %s failed (HTTP %d)", e.Operation, e.HTTPStatus)
}

func (e *APIError) Unwrap() error {
	if e.HTTPStatus == http.StatusNotFound {
		return ErrNotFound
	}
	return nil
}

// ClientOptions selects the official control API and sandbox traffic origins.
// SandboxURL is optional: Embed uses its proxy origin, while Cloud can use the
// per-sandbox domain returned by the provider. HTTP redirects are never followed.
type ClientOptions struct {
	APIURL     string
	SandboxURL string
	Domain     string
	APIKey     string
	HTTPClient *http.Client
}

type Client struct {
	apiOrigin     *url.URL
	sandboxOrigin *url.URL
	domain        string
	apiKey        string
	http          *http.Client
}

// Sandbox is the provider identity and resource profile. Credentials are kept
// separate from the wire DTO so ordinary JSON persistence cannot publish them.
type Sandbox struct {
	ID                 string            `json:"sandboxID"`
	TemplateID         string            `json:"templateID"`
	Alias              string            `json:"alias,omitempty"`
	Domain             string            `json:"domain,omitempty"`
	EnvdVersion        string            `json:"envdVersion"`
	EnvdAccessToken    string            `json:"-"`
	TrafficAccessToken string            `json:"-"`
	State              string            `json:"state,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	CPUCount           int               `json:"cpuCount"`
	MemoryMB           int64             `json:"memoryMB"`
	DiskSizeMB         int64             `json:"diskSizeMB"`
	StartedAt          time.Time         `json:"startedAt"`
	EndAt              time.Time         `json:"endAt"`
}

func (s Sandbox) String() string   { return "E2B sandbox (credentials omitted)" }
func (s Sandbox) GoString() string { return s.String() }

type sandboxWire struct {
	ID                 string            `json:"sandboxID"`
	TemplateID         string            `json:"templateID"`
	Alias              string            `json:"alias"`
	Domain             string            `json:"domain"`
	EnvdVersion        string            `json:"envdVersion"`
	EnvdAccessToken    string            `json:"envdAccessToken"`
	TrafficAccessToken string            `json:"trafficAccessToken"`
	State              string            `json:"state"`
	Metadata           map[string]string `json:"metadata"`
	CPUCount           int               `json:"cpuCount"`
	MemoryMB           int64             `json:"memoryMB"`
	DiskSizeMB         int64             `json:"diskSizeMB"`
	StartedAt          time.Time         `json:"startedAt"`
	EndAt              time.Time         `json:"endAt"`
}

func (s sandboxWire) sandbox() Sandbox {
	return Sandbox(s)
}

type CreateOptions struct {
	TemplateID     string
	TimeoutSeconds int
	EnvVars        map[string]string
	Metadata       map[string]string
}

var providerIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var providerDomainPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
var bootstrapEnvdVersionPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

const maxControlResponseBytes = 4 << 20

func NewClient(opts ClientOptions) (*Client, error) {
	if strings.TrimSpace(opts.APIKey) == "" || strings.ContainsAny(opts.APIKey, "\r\n") {
		return nil, errors.New("E2B API key is required and must be a valid header value")
	}
	domain := opts.Domain
	if domain == "" {
		domain = "e2b.app"
	}
	if !validProviderDomain(domain) {
		return nil, errors.New("invalid E2B domain")
	}
	origin := opts.APIURL
	if origin == "" {
		origin = "https://api." + domain
	}
	apiOrigin, err := parseOrigin(origin)
	if err != nil {
		return nil, errors.New("invalid E2B API origin")
	}
	var sandboxOrigin *url.URL
	if opts.SandboxURL != "" {
		sandboxOrigin, err = parseOrigin(opts.SandboxURL)
		if err != nil {
			return nil, errors.New("invalid E2B sandbox origin")
		}
	}
	client := &http.Client{Timeout: 60 * time.Second}
	if opts.HTTPClient != nil {
		copyClient := *opts.HTTPClient
		client = &copyClient
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{apiOrigin: apiOrigin, sandboxOrigin: sandboxOrigin, domain: domain,
		apiKey: opts.APIKey, http: client}, nil
}

func parseOrigin(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, errors.New("expected an HTTP origin")
	}
	if p := u.Port(); p != "" {
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid origin port")
		}
	}
	u.Path = ""
	u.RawPath = ""
	return u, nil
}

func validProviderDomain(domain string) bool {
	if !providerDomainPattern.MatchString(domain) || strings.Contains(domain, "..") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

// Create follows the official Python 2.49.1 create contract. There are no
// implicit retries: a timed-out POST may already have created a sandbox.
// A non-nil sandbox can accompany a credential-validation error; its identity
// lets the owning driver verify metadata and clean up that partial creation.
func (c *Client) Create(ctx context.Context, opts CreateOptions) (*Sandbox, error) {
	if strings.TrimSpace(opts.TemplateID) == "" || opts.TimeoutSeconds < 1 {
		return nil, errors.New("E2B template and positive timeout are required")
	}
	body := struct {
		TemplateID          string            `json:"templateID"`
		Timeout             int               `json:"timeout"`
		Secure              bool              `json:"secure"`
		AutoPause           bool              `json:"autoPause"`
		AutoResume          map[string]bool   `json:"autoResume"`
		AllowInternetAccess bool              `json:"allow_internet_access"`
		Network             map[string]bool   `json:"network"`
		EnvVars             map[string]string `json:"envVars"`
		Metadata            map[string]string `json:"metadata"`
	}{opts.TemplateID, opts.TimeoutSeconds, true, false, map[string]bool{"enabled": false},
		true, map[string]bool{"allowPublicTraffic": false}, opts.EnvVars, opts.Metadata}
	var wire sandboxWire
	_, err := c.control(ctx, "create", http.MethodPost, "/sandboxes", nil, body, &wire, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	sb := wire.sandbox()
	if err = validateCredentials(&sb); err != nil {
		return &sb, err
	}
	return &sb, nil
}

func validateCredentials(sb *Sandbox) error {
	if sb == nil || !providerIDPattern.MatchString(sb.ID) || sb.EnvdVersion == "" {
		return errors.New("E2B returned an invalid sandbox identity or envd version")
	}
	version := bootstrapEnvdVersionPattern.FindStringSubmatch(sb.EnvdVersion)
	if version == nil {
		return errors.New("E2B returned an unsupported envd version; bootstrap requires at least 0.3.0")
	}
	major, majorErr := strconv.Atoi(version[1])
	minor, minorErr := strconv.Atoi(version[2])
	patch, patchErr := strconv.Atoi(version[3])
	if majorErr != nil || minorErr != nil || patchErr != nil ||
		(major == 0 && (minor < 3 || (minor == 3 && patch == 0 && version[4] != ""))) {
		return errors.New("E2B returned an unsupported envd version; bootstrap requires at least 0.3.0")
	}
	if sb.EnvdAccessToken == "" || sb.TrafficAccessToken == "" ||
		strings.ContainsAny(sb.EnvdAccessToken+sb.TrafficAccessToken, "\r\n") {
		return errors.New("E2B private sandbox requires envd and traffic access tokens")
	}
	return nil
}

func sandboxPath(id string) (string, error) {
	if !providerIDPattern.MatchString(id) {
		return "", errors.New("invalid E2B sandbox ID")
	}
	return "/sandboxes/" + id, nil
}

func (c *Client) Get(ctx context.Context, id string) (*Sandbox, error) {
	path, err := sandboxPath(id)
	if err != nil {
		return nil, err
	}
	var wire sandboxWire
	if _, err = c.control(ctx, "get", http.MethodGet, path, nil, nil, &wire, http.StatusOK); err != nil {
		return nil, err
	}
	if wire.ID != id {
		return nil, errors.New("E2B sandbox response identity mismatch")
	}
	sb := wire.sandbox()
	return &sb, nil
}

// List filters on the provider and checks every result locally. Callers must
// supply their reserved owner metadata; unrelated sandboxes are never returned.
func (c *Client) List(ctx context.Context, metadata map[string]string) ([]Sandbox, error) {
	if len(metadata) == 0 {
		return nil, errors.New("E2B list requires ownership metadata")
	}
	filter := make(url.Values)
	for k, v := range metadata {
		filter.Set(k, v)
	}
	query := url.Values{"metadata": {filter.Encode()}, "limit": {"100"}, "state": {"running,paused"}}
	result := make([]Sandbox, 0)
	seen := make(map[string]bool)
	seenCursors := make(map[string]bool)
	for {
		var page []sandboxWire
		headers, err := c.control(ctx, "list", http.MethodGet, "/v2/sandboxes", query, nil, &page, http.StatusOK)
		if err != nil {
			return nil, err
		}
		for _, wire := range page {
			if !providerIDPattern.MatchString(wire.ID) {
				return nil, errors.New("E2B list returned an invalid sandbox identity")
			}
			owned := true
			for k, v := range metadata {
				actual, present := wire.Metadata[k]
				if !present || actual != v {
					owned = false
					break
				}
			}
			if owned && !seen[wire.ID] {
				result = append(result, wire.sandbox())
				seen[wire.ID] = true
			}
		}
		next := headers.Get("X-Next-Token")
		if next == "" {
			return result, nil
		}
		if seenCursors[next] {
			return nil, errors.New("E2B list returned a repeated pagination cursor")
		}
		seenCursors[next] = true
		query.Set("nextToken", next)
	}
}

func (c *Client) Delete(ctx context.Context, id string) error {
	path, err := sandboxPath(id)
	if err != nil {
		return err
	}
	_, err = c.control(ctx, "delete", http.MethodDelete, path, nil, nil, nil, http.StatusNoContent)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (c *Client) SetTimeout(ctx context.Context, id string, seconds int) error {
	if seconds < 1 {
		return errors.New("E2B timeout must be positive")
	}
	path, err := sandboxPath(id)
	if err != nil {
		return err
	}
	_, err = c.control(ctx, "set timeout", http.MethodPost, path+"/timeout", nil,
		map[string]int{"timeout": seconds}, nil, http.StatusNoContent)
	return err
}

// Pause requests a full memory snapshot and refuses unhealthy-guest snapshots.
func (c *Client) Pause(ctx context.Context, id string) error {
	path, err := sandboxPath(id)
	if err != nil {
		return err
	}
	_, err = c.control(ctx, "pause", http.MethodPost, path+"/pause", nil,
		map[string]bool{"memory": true, "requireHealthyGuest": true}, nil, http.StatusNoContent)
	return err
}

// Connect is explicit: it can resume a paused guest and extend its lease. It
// must not be used by Get/List or a read-only reconciliation path.
func (c *Client) Connect(ctx context.Context, id string, timeoutSeconds int) (*Sandbox, error) {
	if timeoutSeconds < 1 {
		return nil, errors.New("E2B connect timeout must be positive")
	}
	path, err := sandboxPath(id)
	if err != nil {
		return nil, err
	}
	var wire sandboxWire
	_, err = c.control(ctx, "connect", http.MethodPost, path+"/connect", nil,
		struct {
			Timeout int  `json:"timeout"`
			Memory  bool `json:"memory"`
		}{timeoutSeconds, true}, &wire, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	sb := wire.sandbox()
	if sb.ID != id {
		return nil, errors.New("E2B connect response identity mismatch")
	}
	if err = validateCredentials(&sb); err != nil {
		return &sb, err
	}
	return &sb, nil
}

// SandboxEndpoint never carries the API key. For Cloud, the hostname routes the
// port; custom origins additionally support Embed's official routing headers.
func (c *Client) SandboxEndpoint(sb *Sandbox, port int) (*url.URL, http.Header, error) {
	if sb == nil || !providerIDPattern.MatchString(sb.ID) || port < 1 || port > 65535 ||
		sb.TrafficAccessToken == "" || strings.ContainsAny(sb.TrafficAccessToken, "\r\n") {
		return nil, nil, errors.New("invalid E2B private traffic credentials or port")
	}
	var target url.URL
	if c.sandboxOrigin != nil {
		target = *c.sandboxOrigin
	} else {
		domain := sb.Domain
		if domain == "" {
			domain = c.domain
		}
		if !validProviderDomain(domain) {
			return nil, nil, errors.New("E2B returned an invalid sandbox domain")
		}
		target = url.URL{Scheme: "https", Host: strconv.Itoa(port) + "-" + sb.ID + "." + domain}
	}
	headers := make(http.Header)
	headers.Set("E2b-Sandbox-Id", sb.ID)
	headers.Set("E2b-Sandbox-Port", strconv.Itoa(port))
	headers.Set("e2b-traffic-access-token", sb.TrafficAccessToken)
	return &target, headers, nil
}

func (c *Client) control(ctx context.Context, operation, method, path string, query url.Values, body, output any, statuses ...int) (http.Header, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, errors.New("E2B request encoding failed")
		}
		input = bytes.NewReader(data)
	}
	u := *c.apiOrigin
	u.Path, u.RawQuery = path, query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), input)
	if err != nil {
		return nil, errors.New("E2B request construction failed")
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "grasp-e2b-driver/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, safeTransportError(ctx, operation, err)
	}
	defer func() { _ = response.Body.Close() }()
	ok := false
	for _, status := range statuses {
		ok = ok || status == response.StatusCode
	}
	if !ok {
		return nil, &APIError{Operation: operation, HTTPStatus: response.StatusCode}
	}
	if output != nil {
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxControlResponseBytes+1))
		if readErr != nil {
			return nil, safeTransportError(ctx, operation, readErr)
		}
		if len(data) > maxControlResponseBytes || json.Unmarshal(data, output) != nil {
			return nil, errors.New("E2B returned an invalid or oversized JSON response")
		}
	}
	return response.Header.Clone(), nil
}

func safeTransportError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("E2B %s request interrupted: %w", operation, ctx.Err())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("E2B %s request timed out: %w", operation, context.DeadlineExceeded)
	}
	return fmt.Errorf("E2B %s transport failed", operation)
}
