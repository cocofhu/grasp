package codexoauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrNotAuthFile means the text is not a JSON object.
var ErrNotAuthFile = errors.New("not a Codex auth.json object")

// AuthFile is a Codex auth.json. Fields it does not know are kept as-is so a
// newer CLI's additions survive a platform refresh.
type AuthFile struct {
	top    map[string]json.RawMessage
	tokens map[string]json.RawMessage
}

// ParseAuthFile parses raw auth.json text.
func ParseAuthFile(raw string) (*AuthFile, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &top); err != nil || top == nil {
		return nil, ErrNotAuthFile
	}
	a := &AuthFile{top: top, tokens: map[string]json.RawMessage{}}
	if rawTokens, ok := top["tokens"]; ok && string(rawTokens) != "null" {
		if err := json.Unmarshal(rawTokens, &a.tokens); err != nil {
			return nil, ErrNotAuthFile
		}
		if a.tokens == nil {
			a.tokens = map[string]json.RawMessage{}
		}
	}
	return a, nil
}

// NewAuthFile builds a ChatGPT-mode auth.json from a fresh sign-in.
func NewAuthFile(t Tokens, now time.Time) *AuthFile {
	a := &AuthFile{top: map[string]json.RawMessage{}, tokens: map[string]json.RawMessage{}}
	a.ApplyTokens(t, now)
	return a
}

func (a *AuthFile) clone() *AuthFile {
	out := &AuthFile{top: make(map[string]json.RawMessage, len(a.top)), tokens: make(map[string]json.RawMessage, len(a.tokens))}
	for k, v := range a.top {
		out.top[k] = v
	}
	for k, v := range a.tokens {
		out.tokens[k] = v
	}
	return out
}

func rawString(m map[string]json.RawMessage, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// AccessToken is tokens.access_token.
func (a *AuthFile) AccessToken() string { return rawString(a.tokens, "access_token") }

// RefreshToken is tokens.refresh_token.
func (a *AuthFile) RefreshToken() string { return rawString(a.tokens, "refresh_token") }

// IDToken is tokens.id_token.
func (a *AuthFile) IDToken() string { return rawString(a.tokens, "id_token") }

// AuthMode is the auth_mode field.
func (a *AuthFile) AuthMode() string { return rawString(a.top, "auth_mode") }

// ApplyTokens stores a token response. A response without a refresh token keeps
// the previous one; last_refresh becomes now.
func (a *AuthFile) ApplyTokens(t Tokens, now time.Time) {
	if t.IDToken != "" {
		a.tokens["id_token"] = mustRaw(t.IDToken)
	}
	if t.AccessToken != "" {
		a.tokens["access_token"] = mustRaw(t.AccessToken)
	}
	if t.RefreshToken != "" {
		a.tokens["refresh_token"] = mustRaw(t.RefreshToken)
	} else if _, ok := a.tokens["refresh_token"]; !ok {
		a.tokens["refresh_token"] = mustRaw("")
	}
	if id := accountID(a.IDToken()); id != "" {
		a.tokens["account_id"] = mustRaw(id)
	}
	if a.AuthMode() == "" {
		a.top["auth_mode"] = mustRaw("chatgpt")
	}
	if _, ok := a.top["OPENAI_API_KEY"]; !ok {
		a.top["OPENAI_API_KEY"] = json.RawMessage("null")
	}
	a.top["last_refresh"] = mustRaw(now.UTC().Format(time.RFC3339Nano))
}

// Encode renders auth.json the way the Codex CLI writes it (indented JSON).
func (a *AuthFile) Encode() (string, error) {
	top := make(map[string]json.RawMessage, len(a.top))
	for k, v := range a.top {
		top[k] = v
	}
	if len(a.tokens) > 0 {
		tokens, err := json.Marshal(a.tokens)
		if err != nil {
			return "", err
		}
		top["tokens"] = tokens
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// SandboxCopy is auth.json with an empty refresh token: a sandbox can call the
// model with the access token but can never rotate the platform's login. It
// must be "" rather than null or absent: codex-cli 0.164 then reports a 401 as
// "Failed to refresh token", which the platform recognizes and recovers from.
func (a *AuthFile) SandboxCopy() (string, error) {
	c := a.clone()
	if len(c.tokens) > 0 {
		c.tokens["refresh_token"] = mustRaw("")
	}
	return c.Encode()
}

// AccessExpiry is the access token's exp claim.
func (a *AuthFile) AccessExpiry() (time.Time, bool) {
	claims, ok := JWTClaims(a.AccessToken())
	if !ok {
		return time.Time{}, false
	}
	exp, ok := claims["exp"].(float64)
	if !ok || exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(exp), 0), true
}

// LastRefresh is the last_refresh timestamp.
func (a *AuthFile) LastRefresh() (time.Time, bool) {
	s := rawString(a.top, "last_refresh")
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Email is the account email from the id_token (or the access token profile
// claim when the id_token lacks it).
func (a *AuthFile) Email() string {
	if claims, ok := JWTClaims(a.IDToken()); ok {
		if email, _ := claims["email"].(string); email != "" {
			return email
		}
	}
	if claims, ok := JWTClaims(a.AccessToken()); ok {
		if profile, ok := claims["https://api.openai.com/profile"].(map[string]any); ok {
			if email, _ := profile["email"].(string); email != "" {
				return email
			}
		}
	}
	return ""
}

func accountID(idToken string) string {
	claims, ok := JWTClaims(idToken)
	if !ok {
		return ""
	}
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if id, _ := auth["chatgpt_account_id"].(string); id != "" {
			return id
		}
	}
	return ""
}

// JWTClaims decodes a JWT payload without verifying it; the platform only reads
// expiry and display claims from tokens it received from the issuer itself.
func JWTClaims(token string) (map[string]any, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[1] == "" {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil || claims == nil {
		return nil, false
	}
	return claims, true
}

// SandboxCopyOf strips the refresh token from raw auth.json text.
func SandboxCopyOf(raw string) (string, error) {
	a, err := ParseAuthFile(raw)
	if err != nil {
		return "", err
	}
	return a.SandboxCopy()
}
