package qqbot

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"backend/internal/service"
)

func (s *Service) downloadImage(ctx context.Context, rawURL string, limit int64) (service.PromptImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return service.PromptImage{}, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return service.PromptImage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return service.PromptImage{}, fmt.Errorf("image status=%d", resp.StatusCode)
	}
	var r io.Reader = resp.Body
	if limit > 0 {
		r = io.LimitReader(resp.Body, limit+1)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return service.PromptImage{}, err
	}
	if limit > 0 && int64(len(b)) > limit {
		return service.PromptImage{}, fmt.Errorf("image exceeds %d bytes", limit)
	}
	mimeType := resp.Header.Get("Content-Type")
	if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(b)
	}
	exts, _ := mime.ExtensionsByType(mimeType)
	name := "qq-image"
	if len(exts) > 0 {
		name += exts[0]
	}
	return service.PromptImage{
		Data:     base64.StdEncoding.EncodeToString(b),
		MimeType: mimeType,
		Name:     name,
	}, nil
}

func extractImageURLs(data map[string]any) []string {
	var out []string
	collectImageURLs(data["attachments"], &out)
	collectImageURLs(data["images"], &out)
	collectImageURLs(data["image"], &out)
	if msg, ok := data["message"].(map[string]any); ok {
		collectImageURLs(msg["attachments"], &out)
		collectImageURLs(msg["images"], &out)
	}
	seen := make(map[string]struct{}, len(out))
	uniq := out[:0]
	for _, u := range out {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		uniq = append(uniq, u)
	}
	return uniq
}

func collectImageURLs(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		if looksLikeImageURL(x) {
			*out = append(*out, x)
		}
	case []any:
		for _, item := range x {
			collectImageURLs(item, out)
		}
	case map[string]any:
		ct := strings.ToLower(firstString(x["content_type"], x["contentType"], x["mime_type"], x["mimeType"]))
		for _, key := range []string{"url", "image_url", "imageUrl"} {
			if s := firstString(x[key]); s != "" && (strings.HasPrefix(ct, "image/") || (ct == "" && looksLikeImageURL(s))) {
				*out = append(*out, s)
			}
		}
	}
}

func looksLikeImageURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return (strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")) &&
		(strings.Contains(low, ".png") || strings.Contains(low, ".jpg") || strings.Contains(low, ".jpeg") || strings.Contains(low, ".gif") || strings.Contains(low, ".webp"))
}
