package codexoauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func jwt(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

func TestParseAuthFileRejectsNonObjects(t *testing.T) {
	for _, raw := range []string{"", "x", "[]", "null", `{"tokens":"x"}`} {
		if _, err := ParseAuthFile(raw); err == nil {
			t.Errorf("%q: want error", raw)
		}
	}
	a, err := ParseAuthFile(`{"tokens":null}`)
	if err != nil || a.AccessToken() != "" {
		t.Fatalf("null tokens: %v", err)
	}
}

func TestApplyTokensKeepsUnknownFieldsAndRefreshToken(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 123, time.UTC)
	raw := `{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"future":{"x":1},"tokens":{"id_token":"old","access_token":"a1","refresh_token":"r1","account_id":"acc","extra":"keep"},"last_refresh":"2026-10-01T00:00:00Z"}`
	a, err := ParseAuthFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a.RefreshToken() != "r1" || a.AccessToken() != "a1" || a.IDToken() != "old" || a.AuthMode() != "chatgpt" {
		t.Fatal("accessors")
	}
	id := jwt(t, map[string]any{"email": "dev@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acc-2"}})
	a.ApplyTokens(Tokens{IDToken: id, AccessToken: "a2"}, now)
	out, err := a.Encode()
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, out)
	tokens := doc["tokens"].(map[string]any)
	if tokens["refresh_token"] != "r1" || tokens["access_token"] != "a2" || tokens["extra"] != "keep" || tokens["account_id"] != "acc-2" {
		t.Fatalf("tokens %v", tokens)
	}
	if doc["future"].(map[string]any)["x"].(float64) != 1 {
		t.Fatal("unknown top-level field dropped")
	}
	if doc["last_refresh"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("last_refresh %v", doc["last_refresh"])
	}
	if lr, ok := a.LastRefresh(); !ok || !lr.Equal(now) {
		t.Fatalf("LastRefresh %v %v", lr, ok)
	}
	if a.Email() != "dev@example.com" {
		t.Fatalf("email %q", a.Email())
	}
	a.ApplyTokens(Tokens{AccessToken: "a3", RefreshToken: "r3"}, now)
	if a.RefreshToken() != "r3" {
		t.Fatal("rotated refresh token not stored")
	}
}

func TestNewAuthFileIsCodexShaped(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	a := NewAuthFile(Tokens{IDToken: "i", AccessToken: "a", RefreshToken: "r"}, now)
	out, err := a.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "\n") || !strings.Contains(out, "\n  ") {
		t.Fatalf("not indented: %q", out)
	}
	doc := decode(t, out)
	if doc["auth_mode"] != "chatgpt" || doc["OPENAI_API_KEY"] != nil {
		t.Fatalf("doc %v", doc)
	}
	if _, ok := doc["OPENAI_API_KEY"]; !ok {
		t.Fatal("OPENAI_API_KEY must be present as null")
	}
	tokens := doc["tokens"].(map[string]any)
	if tokens["refresh_token"] != "r" || tokens["access_token"] != "a" || tokens["id_token"] != "i" {
		t.Fatalf("tokens %v", tokens)
	}
	empty := NewAuthFile(Tokens{AccessToken: "a"}, now)
	if _, ok := empty.tokens["refresh_token"]; !ok || empty.RefreshToken() != "" {
		t.Fatal("refresh_token key must exist")
	}
}

func TestSandboxCopyDropsOnlyRefreshToken(t *testing.T) {
	raw := `{"auth_mode":"chatgpt","tokens":{"id_token":"i","access_token":"a","refresh_token":"secret-rt","account_id":"x"},"last_refresh":"2026-11-01T00:00:00Z"}`
	copyText, err := SandboxCopyOf(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(copyText, "secret-rt") {
		t.Fatal("sandbox copy leaks refresh token")
	}
	doc := decode(t, copyText)
	tokens := doc["tokens"].(map[string]any)
	if tokens["refresh_token"] != "" || tokens["access_token"] != "a" || tokens["id_token"] != "i" || tokens["account_id"] != "x" {
		t.Fatalf("tokens %v", tokens)
	}
	if doc["last_refresh"] != "2026-11-01T00:00:00Z" {
		t.Fatal("last_refresh changed")
	}
	a, _ := ParseAuthFile(raw)
	if a.RefreshToken() != "secret-rt" {
		t.Fatal("SandboxCopy mutated the original")
	}
	noTokens, err := SandboxCopyOf(`{"auth_mode":"chatgpt"}`)
	if err != nil || strings.Contains(noTokens, "tokens") {
		t.Fatalf("no tokens: %q %v", noTokens, err)
	}
	if _, err := SandboxCopyOf("x"); err == nil {
		t.Fatal("non JSON must fail")
	}
}

func TestClaims(t *testing.T) {
	exp := time.Unix(1_900_000_000, 0)
	at := jwt(t, map[string]any{"exp": exp.Unix(), "https://api.openai.com/profile": map[string]any{"email": "p@example.com"}})
	a := NewAuthFile(Tokens{AccessToken: at}, time.Now())
	if got, ok := a.AccessExpiry(); !ok || !got.Equal(exp) {
		t.Fatalf("expiry %v %v", got, ok)
	}
	if a.Email() != "p@example.com" {
		t.Fatalf("profile email %q", a.Email())
	}
	b := NewAuthFile(Tokens{AccessToken: "opaque"}, time.Now())
	if _, ok := b.AccessExpiry(); ok {
		t.Fatal("opaque token has no expiry")
	}
	if b.Email() != "" {
		t.Fatal("no email expected")
	}
	c := NewAuthFile(Tokens{AccessToken: jwt(t, map[string]any{"exp": "soon"})}, time.Now())
	if _, ok := c.AccessExpiry(); ok {
		t.Fatal("string exp is invalid")
	}
	for _, tok := range []string{"a.b", "a..c", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte("[]")) + ".c"} {
		if _, ok := JWTClaims(tok); ok {
			t.Errorf("%q must not decode", tok)
		}
	}
	d, _ := ParseAuthFile(`{"last_refresh":"yesterday"}`)
	if _, ok := d.LastRefresh(); ok {
		t.Fatal("bad last_refresh must not parse")
	}
	if _, ok := (&AuthFile{top: map[string]json.RawMessage{}}).LastRefresh(); ok {
		t.Fatal("missing last_refresh")
	}
	if rawString(map[string]json.RawMessage{"k": json.RawMessage("1")}, "k") != "" {
		t.Fatal("non-string")
	}
	if accountID("x") != "" {
		t.Fatal("bad id token")
	}
}
