package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WaitReady gates private-endpoint startup before any database or engine
// reconciliation. A listening gateway must confirm both readiness and the
// expected driver: a different process on the same port is not sufficient.
// The health request deliberately carries no gateway API key, uses no proxy,
// follows no redirects and never includes response bodies or URLs in errors.
func (g *GatewayClient) WaitReady(ctx context.Context, expectedDriver string) error {
	if g == nil || strings.TrimSpace(expectedDriver) == "" {
		return errors.New("private gateway readiness configuration is invalid")
	}
	target, err := url.Parse(g.baseURL + "/healthz")
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") ||
		target.User != nil || target.RawQuery != "" || target.ForceQuery || target.Fragment != "" || target.Opaque != "" {
		return errors.New("private gateway readiness requires an HTTP(S) base URL without credentials or query")
	}
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
	}
	client := &http.Client{
		Transport: transport, Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("private gateway readiness failed: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return errors.New("private gateway readiness request is invalid")
		}
		req.Header.Set("Accept", "application/json")
		if response, err := client.Do(req); err == nil {
			ready := privateGatewayReadyResponse(response, expectedDriver)
			_ = response.Body.Close()
			if ready {
				return nil
			}
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("private gateway readiness failed: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func privateGatewayReadyResponse(response *http.Response, expectedDriver string) bool {
	if response.StatusCode != http.StatusOK {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 {
		return false
	}
	var health struct {
		Status string `json:"status"`
		Driver string `json:"driver"`
	}
	return json.Unmarshal(data, &health) == nil && health.Status == "ok" && health.Driver == expectedDriver
}
