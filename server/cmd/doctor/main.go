package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
)

type options struct {
	apiURL        string
	gatewayURL    string
	gatewayAPIKey string
	doctorToken   string
	timeout       time.Duration
	runDemo       bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "doctor failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "doctor" {
		args = args[1:]
	}
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	if err := checkHTTP(ctx, opts.apiURL+"/api/health", ""); err != nil {
		return fmt.Errorf("service API: %w", err)
	}
	_, _ = fmt.Fprintln(out, "Service API .............. healthy")
	if !opts.runDemo {
		return nil
	}

	if err := checkHTTP(ctx, opts.gatewayURL+"/healthz", opts.gatewayAPIKey); err != nil {
		return fmt.Errorf("sandbox gateway: %w", err)
	}
	_, _ = fmt.Fprintln(out, "Sandbox gateway .......... healthy")

	client := sandbox.NewGatewayClient(opts.gatewayURL, opts.gatewayAPIKey)
	created, err := client.Create(ctx, sandbox.GWCreateRequest{
		Labels: map[string]string{
			"approving.doctor": "true",
		},
		Ports: []int{8765},
	})
	if err != nil {
		return fmt.Errorf("create demo sandbox: %w", err)
	}
	if strings.TrimSpace(created.ID) == "" {
		return errors.New("create demo sandbox: gateway returned an empty id")
	}
	cleanup := func() error {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		return client.Destroy(cleanupCtx, created.ID)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			_ = cleanup()
		}
	}()

	if _, err := client.WaitRunning(ctx, created.ID, remaining(ctx, 2*time.Minute)); err != nil {
		return fmt.Errorf("wait for demo sandbox: %w", err)
	}
	_, _ = fmt.Fprintln(out, "Demo sandbox ............. passed")

	if err := verifyArtifactIsolation(ctx, opts.apiURL, opts.doctorToken); err != nil {
		return fmt.Errorf("artifact isolation: %w", err)
	}
	_, _ = fmt.Fprintln(out, "Artifact isolation ....... verified")

	if err := cleanup(); err != nil {
		return fmt.Errorf("delete demo sandbox: %w", err)
	}
	cleaned = true
	_, _ = fmt.Fprintln(out, "Cleanup .................. passed")
	return nil
}

func parseOptions(args []string) (options, error) {
	port := os.Getenv("GRASP_PORT")
	if port == "" {
		port = "8080"
	}
	gatewayURL := os.Getenv("GRASP_SANDBOX_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://127.0.0.1:8899"
	}
	var opts options
	fs := flag.NewFlagSet("grasp doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.apiURL, "api-url", "http://127.0.0.1:"+port, "Grasp base URL")
	fs.StringVar(&opts.gatewayURL, "gateway-url", gatewayURL, "sandbox-gateway base URL")
	fs.StringVar(&opts.gatewayAPIKey, "gateway-api-key", os.Getenv("GRASP_SANDBOX_GATEWAY_API_KEY"), "sandbox-gateway bearer token")
	fs.StringVar(&opts.doctorToken, "doctor-token", os.Getenv("GRASP_DOCTOR_TOKEN"), "local doctor control-plane token")
	fs.DurationVar(&opts.timeout, "timeout", 3*time.Minute, "overall timeout")
	fs.BoolVar(&opts.runDemo, "run-demo", false, "create a sandbox and verify artifact isolation")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.timeout <= 0 {
		return options{}, errors.New("timeout must be positive")
	}
	opts.apiURL = strings.TrimRight(strings.TrimSpace(opts.apiURL), "/")
	opts.gatewayURL = strings.TrimRight(strings.TrimSpace(opts.gatewayURL), "/")
	if opts.apiURL == "" || opts.gatewayURL == "" {
		return options{}, errors.New("api-url and gateway-url must not be empty")
	}
	if opts.runDemo && strings.TrimSpace(opts.doctorToken) == "" {
		return options{}, errors.New("GRASP_DOCTOR_TOKEN or --doctor-token is required with --run-demo")
	}
	return opts, nil
}

func checkHTTP(ctx context.Context, url, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	return nil
}

type artifactSession struct {
	ID           string `json:"id"`
	RunA         string `json:"run_a"`
	TokenA       string `json:"token_a"`
	RunB         string `json:"run_b"`
	TokenB       string `json:"token_b"`
	CleanupToken string `json:"cleanup_token"`
}

type rpcResponse struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
}

func verifyArtifactIsolation(ctx context.Context, apiURL, doctorToken string) error {
	session, err := startArtifactSession(ctx, apiURL, doctorToken)
	if err != nil {
		return err
	}
	cleaned := false
	defer func() {
		if !cleaned {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = cleanupArtifactSession(cleanupCtx, apiURL, doctorToken, session)
		}
	}()

	name := "doctor-" + uuid.NewString() + ".txt"
	const content = "grasp-doctor"
	write, status, err := callMCP(ctx, apiURL, session.RunA, session.TokenA, 1, "write_artifact", map[string]any{
		"name": name, "content": content, "kind": "text",
	})
	if err != nil || status != http.StatusOK || write.Result.IsError {
		return fmt.Errorf("same-run write failed")
	}
	read, status, err := callMCP(ctx, apiURL, session.RunA, session.TokenA, 2, "read_artifact", map[string]any{"name": name})
	if err != nil || status != http.StatusOK || read.Result.IsError || len(read.Result.Content) == 0 || read.Result.Content[0].Text != content {
		return errors.New("same-run read failed")
	}
	_, status, err = callMCP(ctx, apiURL, session.RunB, session.TokenA, 3, "read_artifact", map[string]any{"name": name})
	if err != nil || status != http.StatusUnauthorized {
		return errors.New("cross-run token was not rejected")
	}
	read, status, err = callMCP(ctx, apiURL, session.RunB, session.TokenB, 4, "read_artifact", map[string]any{"name": name})
	if err != nil || status != http.StatusOK || !read.Result.IsError {
		return errors.New("artifact was visible in another run")
	}
	if err := cleanupArtifactSession(ctx, apiURL, doctorToken, session); err != nil {
		return err
	}
	cleaned = true
	return nil
}

func startArtifactSession(ctx context.Context, apiURL, doctorToken string) (artifactSession, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/_internal/doctor/artifact-sessions", nil)
	if err != nil {
		return artifactSession{}, err
	}
	req.Header.Set("Authorization", "Bearer "+doctorToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return artifactSession{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var session artifactSession
	if resp.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return artifactSession{}, fmt.Errorf("start artifact session returned %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&session); err != nil {
		return artifactSession{}, err
	}
	if session.ID == "" || session.RunA == "" || session.TokenA == "" || session.RunB == "" || session.TokenB == "" || session.CleanupToken == "" {
		return artifactSession{}, errors.New("start artifact session returned incomplete credentials")
	}
	return session, nil
}

func cleanupArtifactSession(ctx context.Context, apiURL, doctorToken string, session artifactSession) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL+"/_internal/doctor/artifact-sessions/"+session.ID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+doctorToken)
	req.Header.Set("X-Grasp-Doctor-Cleanup", session.CleanupToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("cleanup artifact session returned %s", resp.Status)
	}
	return nil
}

func callMCP(ctx context.Context, apiURL, runID, token string, id int, tool string, arguments map[string]any) (rpcResponse, int, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": arguments,
		},
	})
	if err != nil {
		return rpcResponse{}, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/mcp/runs/"+runID, bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rpcResponse{}, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return rpcResponse{}, resp.StatusCode, nil
	}
	var out rpcResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return rpcResponse{}, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}

func remaining(ctx context.Context, maximum time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return maximum
	}
	left := time.Until(deadline)
	if left < maximum {
		return left
	}
	return maximum
}
