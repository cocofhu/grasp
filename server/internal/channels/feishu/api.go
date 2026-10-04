package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/cocofhu/grasp/internal/channels"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

type tokenResp struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
}

type botInfoResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Bot  struct {
		OpenID string `json:"open_id"`
	} `json:"bot"`
}

func probeTenantToken(ctx context.Context, baseURL, appID, secret string) (string, error) {
	body, _ := json.Marshal(map[string]string{"app_id": appID, "app_secret": secret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/open-apis/auth/v3/tenant_access_token/internal",
		bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out tokenResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%w: 解析凭证响应失败", channels.ErrAdapterAuth)
	}
	if out.Code != 0 || strings.TrimSpace(out.TenantAccessToken) == "" {
		msg := strings.TrimSpace(out.Msg)
		if msg == "" {
			msg = fmt.Sprintf("code=%d", out.Code)
		}
		return "", fmt.Errorf("%w: %s", channels.ErrAdapterAuth, msg)
	}
	return out.TenantAccessToken, nil
}

func fetchBotOpenID(ctx context.Context, baseURL, tenantToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/open-apis/bot/v3/info", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out botInfoResp
	if json.Unmarshal(raw, &out) != nil || out.Code != 0 {
		return ""
	}
	return strings.TrimSpace(out.Bot.OpenID)
}

func sendIM(ctx context.Context, client *lark.Client, chatID, msgType, content string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()
	resp, err := client.Im.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu send %s: %s", msgType, resp.Msg)
	}
	return nil
}

func downloadResource(ctx context.Context, client *lark.Client, messageID, fileKey string) ([]byte, string, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(fileKey).
		Type("image").
		Build()
	resp, err := client.Im.MessageResource.Get(ctx, req)
	if err != nil {
		return nil, "", err
	}
	if resp.File == nil {
		return nil, "", fmt.Errorf("feishu resource empty")
	}
	data, err := io.ReadAll(io.LimitReader(resp.File, maxInboundImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxInboundImageBytes {
		return nil, "", errTooLarge
	}
	mime := http.DetectContentType(data)
	return data, mime, nil
}

func uploadImage(ctx context.Context, client *lark.Client, data []byte) (string, error) {
	req := larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType("message").
			Image(bytes.NewReader(data)).
			Build()).
		Build()
	resp, err := client.Im.Image.Create(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() || resp.Data == nil || resp.Data.ImageKey == nil {
		return "", fmt.Errorf("feishu upload image: %s", resp.Msg)
	}
	return *resp.Data.ImageKey, nil
}

var errTooLarge = fmt.Errorf("image exceeds 10 MiB")
