package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrTooManyChats is the bridge refusing a new chat (SANDBOX_MAX_CHATS reached).
var ErrTooManyChats = errors.New("sandbox chat limit reached")

// chatHTTPTimeout bounds one /api/chats call.
var chatHTTPTimeout = 15 * time.Second

// CreateChat opens an extra bridge chat (own Agent session, shared workspace)
// and returns its id for ACPClient.WithChat.
func CreateChat(ctx context.Context, host string, port int, password, title string) (string, error) {
	body, _ := json.Marshal(map[string]string{"title": title})
	resp, err := chatRequest(ctx, http.MethodPost, host, port, password, "/api/chats", body)
	if err != nil {
		return "", err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusConflict {
		return "", ErrTooManyChats
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("create chat: status %d", resp.StatusCode)
	}
	var info struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&info); err != nil {
		return "", fmt.Errorf("create chat: %w", err)
	}
	if strings.TrimSpace(info.ID) == "" {
		return "", errors.New("create chat: empty id")
	}
	return info.ID, nil
}

// DeleteChat ends a chat created by CreateChat. A chat already gone is not an error.
func DeleteChat(ctx context.Context, host string, port int, password, chatID string) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	resp, err := chatRequest(ctx, http.MethodDelete, host, port, password, "/api/chats/"+url.PathEscape(chatID), nil)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete chat: status %d", resp.StatusCode)
	}
	return nil
}

func chatRequest(ctx context.Context, method, host string, port int, password, path string, body []byte) (*http.Response, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	cookie, err := bridgeLogin(ctx, host, port, password)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://%s:%d%s", host, port, path), rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := (&http.Client{Timeout: chatHTTPTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	return resp, nil
}
