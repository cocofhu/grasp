package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Service) SendMarkdown(ctx context.Context, target replyTarget, markdown string) error {
	cfg := s.Config()
	if !cfg.Enabled {
		return errors.New("qqbot disabled")
	}
	token, err := s.accessToken(ctx, cfg)
	if err != nil {
		return err
	}
	endpoint, err := sendEndpoint(target)
	if err != nil {
		return err
	}

	chunks := chunkMarkdownText(markdown, 5000)
	for _, chunk := range chunks {
		payload := map[string]any{
			"msg_type": 2,
			"markdown": map[string]any{
				"content": chunk,
			},
			"msg_seq": nextMsgSeq(),
		}
		if target.MsgID != "" {
			payload["msg_id"] = target.MsgID
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "QQBot "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return err
		}
		respBody, rerr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if rerr != nil {
			return fmt.Errorf("qq api read body: %w", rerr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("qq api status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
	}
	return nil
}

// nextMsgSeq 生成唯一消息序号（与 openclaw-qqbot 一致：时间戳低位 ^ 随机数，0~65535）。
func nextMsgSeq() int {
	t := time.Now().UnixMilli() % 100000000
	r := time.Now().UnixNano() % 65536
	return int((t ^ r) % 65536)
}

// chunkMarkdownText 按 limit 分片 markdown 文本，尽量在行边界或代码块边界拆分。
func chunkMarkdownText(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	remaining := text
	for len(remaining) > 0 {
		if len(remaining) <= limit {
			chunks = append(chunks, remaining)
			break
		}
		cut := findMarkdownBreak(remaining, limit)
		chunks = append(chunks, remaining[:cut])
		remaining = remaining[cut:]
	}
	return chunks
}

// findMarkdownBreak 在 text[:limit] 内寻找合适的拆分点。
func findMarkdownBreak(text string, limit int) int {
	seg := text[:limit]
	// 优先在最后一个换行处拆分
	if idx := strings.LastIndex(seg, "\n"); idx > limit/2 {
		return idx + 1
	}
	// 其次在最后一个空格处拆分
	if idx := strings.LastIndex(seg, " "); idx > limit/2 {
		return idx + 1
	}
	return limit
}

func sendEndpoint(target replyTarget) (string, error) {
	switch target.Kind {
	case "c2c":
		if target.OpenID == "" {
			return "", errors.New("missing openid")
		}
		return apiBase + "/v2/users/" + url.PathEscape(target.OpenID) + "/messages", nil
	case "group":
		if target.GroupOpenID == "" {
			return "", errors.New("missing group_openid")
		}
		return apiBase + "/v2/groups/" + url.PathEscape(target.GroupOpenID) + "/messages", nil
	default:
		return "", fmt.Errorf("unsupported qq target kind %q", target.Kind)
	}
}

// ─── Token 管理 ───

func (s *Service) accessToken(ctx context.Context, cfg Config) (string, error) {
	s.tokenMu.Lock()
	if s.token != "" && time.Until(s.tokenAt) > time.Minute {
		token := s.token
		s.tokenMu.Unlock()
		return token, nil
	}
	s.tokenMu.Unlock()

	body, err := json.Marshal(map[string]string{
		"appId":        cfg.AppID,
		"clientSecret": cfg.AppSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, rerr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if rerr != nil {
		return "", fmt.Errorf("qq token read body: %w", rerr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("qq token status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out tokenResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("qq token response missing access_token")
	}
	expiresIn := parseExpiresIn(out.ExpiresInRaw)
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	exp := time.Now().Add(time.Duration(expiresIn-60) * time.Second)
	s.tokenMu.Lock()
	s.token = out.AccessToken
	s.tokenAt = exp
	s.tokenMu.Unlock()
	return out.AccessToken, nil
}

func (s *Service) getGatewayURL(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gatewayURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if rerr != nil {
		return "", fmt.Errorf("gateway read body: %w", rerr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gateway status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var gw gatewayResponse
	if err := json.Unmarshal(body, &gw); err != nil {
		return "", err
	}
	if gw.URL == "" {
		return "", errors.New("gateway response missing url")
	}
	return gw.URL, nil
}

func parseExpiresIn(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		return 0
	}
}

func (s *Service) invalidateToken() {
	s.tokenMu.Lock()
	s.token = ""
	s.tokenAt = time.Time{}
	s.tokenMu.Unlock()
}
