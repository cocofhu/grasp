package wecom

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors used by cron / run-notify visibility and tests.
var (
	ErrUnspoken        = errors.New("未发言：该会话尚未向机器人发送过消息")
	ErrRateLimited     = errors.New("限流：企业微信频率限制")
	ErrOffline         = errors.New("企业微信长连接未在线")
	ErrOutboundTimeout = errors.New("企业微信出站超时无应答")
)

func classifyWeComError(errcode int, errmsg string) error {
	msg := strings.TrimSpace(errmsg)
	if isRateLimit(errcode, msg) {
		if msg == "" {
			return ErrRateLimited
		}
		return fmt.Errorf("%w（%s）", ErrRateLimited, msg)
	}
	if msg == "" {
		return fmt.Errorf("企业微信错误 %d", errcode)
	}
	return fmt.Errorf("企业微信错误 %d：%s", errcode, msg)
}

func isRateLimit(errcode int, errmsg string) bool {
	switch errcode {
	case 45009, 45011, 45033, 45047, 42001, 45089:
		return true
	}
	lower := strings.ToLower(errmsg)
	return strings.Contains(errmsg, "频率") ||
		strings.Contains(errmsg, "限流") ||
		strings.Contains(lower, "freq") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many")
}

// IsRateLimited reports whether err was classified as WeCom rate limiting.
func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimited)
}
