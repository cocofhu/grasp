package e2b

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
)

const bootstrapEnvdPort = 49983
const maxBootstrapFileBytes = 16 << 20
const maxProcessFrameBytes = 1 << 20

func (c *Client) envdRequest(ctx context.Context, sb *Sandbox, method, route string, body io.Reader) (*http.Request, error) {
	if err := validateCredentials(sb); err != nil {
		return nil, err
	}
	u, headers, err := c.SandboxEndpoint(sb, bootstrapEnvdPort)
	if err != nil {
		return nil, err
	}
	u.Path = route
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errors.New("E2B envd request construction failed")
	}
	req.Header = headers
	req.Header.Set("X-Access-Token", sb.EnvdAccessToken)
	req.Header.Set("User-Agent", "grasp-e2b-driver/1")
	return req, nil
}

// WriteFile is restricted to initial bootstrap files. All ordinary operations
// after bootstrap use the sandbox's authenticated SSH and application services.
// Multipart /files is the official envd protocol, including parent creation.
func (c *Client) WriteFile(ctx context.Context, sb *Sandbox, filePath string, content []byte) error {
	if !strings.HasPrefix(filePath, "/") || strings.ContainsRune(filePath, '\x00') ||
		path.Clean(filePath) == "/" || len(content) > maxBootstrapFileBytes {
		return errors.New("invalid E2B bootstrap file path or size")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", path.Base(filePath))
	if err != nil {
		return errors.New("E2B bootstrap multipart encoding failed")
	}
	if _, err = part.Write(content); err != nil {
		return errors.New("E2B bootstrap multipart encoding failed")
	}
	if err = writer.Close(); err != nil {
		return errors.New("E2B bootstrap multipart encoding failed")
	}
	req, err := c.envdRequest(ctx, sb, http.MethodPost, "/files", &body)
	if err != nil {
		return err
	}
	query := req.URL.Query()
	query.Set("path", filePath)
	query.Set("username", "root")
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := c.http.Do(req)
	if err != nil {
		return safeTransportError(ctx, "bootstrap file upload", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return &APIError{Operation: "bootstrap file upload", HTTPStatus: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxControlResponseBytes+1))
	if err != nil {
		return safeTransportError(ctx, "bootstrap file upload", err)
	}
	var entries []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if len(data) > maxControlResponseBytes || json.Unmarshal(data, &entries) != nil || len(entries) != 1 ||
		entries[0].Path != path.Clean(filePath) || entries[0].Type != "file" {
		return errors.New("E2B bootstrap file upload returned invalid confirmation")
	}
	return nil
}

// StartDetached starts the bootstrap launcher as root and returns only after
// envd emits its initial PID. Closing the response stream does not terminate the
// process (envd intentionally separates process and request cancellation).
// Connect-Timeout-Ms is intentionally omitted: envd treats it as process lifetime.
func (c *Client) StartDetached(ctx context.Context, sb *Sandbox, command string) (uint32, error) {
	if strings.TrimSpace(command) == "" || strings.ContainsRune(command, '\x00') {
		return 0, errors.New("invalid E2B bootstrap command")
	}
	body, err := json.Marshal(struct {
		Process struct {
			Cmd  string   `json:"cmd"`
			Args []string `json:"args"`
		} `json:"process"`
		Stdin bool `json:"stdin"`
	}{Process: struct {
		Cmd  string   `json:"cmd"`
		Args []string `json:"args"`
	}{Cmd: "/bin/bash", Args: []string{"-l", "-c", command}}, Stdin: false})
	if err != nil {
		return 0, errors.New("E2B bootstrap process encoding failed")
	}
	if len(body) > maxProcessFrameBytes {
		return 0, errors.New("E2B bootstrap command is too large")
	}
	frame := make([]byte, 5+len(body))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(body)))
	copy(frame[5:], body)
	req, err := c.envdRequest(ctx, sb, http.MethodPost, "/process.Process/Start", bytes.NewReader(frame))
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth("root", "")
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Keepalive-Ping-Interval", "50")
	response, err := c.http.Do(req)
	if err != nil {
		return 0, safeTransportError(ctx, "bootstrap process start", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return 0, &APIError{Operation: "bootstrap process start", HTTPStatus: response.StatusCode}
	}
	if strings.Split(response.Header.Get("Content-Type"), ";")[0] != "application/connect+json" {
		return 0, errors.New("E2B bootstrap process returned an invalid stream content type")
	}
	var header [5]byte
	if _, err = io.ReadFull(response.Body, header[:]); err != nil {
		return 0, safeTransportError(ctx, "bootstrap process stream", err)
	}
	size := binary.BigEndian.Uint32(header[1:])
	if header[0] != 0 || size == 0 || size > maxProcessFrameBytes {
		// This also rejects Connect end-stream error envelopes (flag2), compressed
		// frames, and future flag bits. Their messages can contain credentials.
		return 0, errors.New("E2B bootstrap process did not return a start event")
	}
	payload := make([]byte, int(size))
	if _, err = io.ReadFull(response.Body, payload); err != nil {
		return 0, safeTransportError(ctx, "bootstrap process stream", err)
	}
	var event struct {
		Event struct {
			Start *struct {
				PID uint32 `json:"pid"`
			} `json:"start"`
			Data      json.RawMessage `json:"data"`
			End       json.RawMessage `json:"end"`
			Keepalive json.RawMessage `json:"keepalive"`
		} `json:"event"`
	}
	if json.Unmarshal(payload, &event) != nil || event.Event.Start == nil || event.Event.Start.PID == 0 ||
		len(event.Event.Data) != 0 || len(event.Event.End) != 0 || len(event.Event.Keepalive) != 0 {
		return 0, errors.New("E2B bootstrap process returned an invalid start event")
	}
	return event.Event.Start.PID, nil
}
