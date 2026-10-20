package gateshare

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateTokenEntropyAndHash(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != TokenHexLen || len(b) != TokenHexLen {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	if a == b {
		t.Fatal("tokens not unique")
	}
	if !ValidTokenShape(a) || !ValidTokenShape(b) {
		t.Fatal("shape")
	}
	ha, hb := HashToken(a), HashToken(b)
	if ha == a || strings.Contains(ha, a[:8]) {
		t.Fatal("hash leaked plaintext")
	}
	if ha == hb {
		t.Fatal("hashes collided")
	}
	if !EqualHash(ha, HashToken(a)) {
		t.Fatal("equal hash")
	}
	if EqualHash(ha, hb) {
		t.Fatal("different hashes compared equal")
	}
	if EqualHash(ha, "deadbeef") || EqualHash("", ha) {
		t.Fatal("short hash must not match")
	}
}

func TestMaskIPAndUA(t *testing.T) {
	if got := MaskIP("203.0.113.45"); got != "203.0.113.x" {
		t.Fatalf("ipv4: %s", got)
	}
	if got := MaskIP("2001:db8::1"); !strings.HasSuffix(got, ":x") {
		t.Fatalf("ipv6: %s", got)
	}
	ua := SummarizeUA("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	if ua != "Chrome / Linux" {
		t.Fatalf("ua: %s", ua)
	}
}

func TestSanitizeStripsInternalURLs(t *testing.T) {
	html := `<a href="/api/blobs/abc">x</a><img src="blob:http://localhost/1"><p>http://10.0.0.5/secret</p>`
	out := SanitizeVisualHTML(html)
	if strings.Contains(out, "/api/blobs") || strings.Contains(out, "blob:") || strings.Contains(out, "10.0.0.5") {
		t.Fatalf("leaked: %s", out)
	}
	desc := SanitizeDescription("see http://127.0.0.1:8080/api/runs/r1 and blob:xx")
	if strings.Contains(desc, "127.0.0.1") || strings.Contains(desc, "/api/runs") || strings.Contains(desc, "blob:") {
		t.Fatalf("desc leaked: %s", desc)
	}
	st := SanitizeStructured("clarified_requirement.json", `{"title":"T","runId":"run-1","goals":["g1"],"env":{"K":"v"}}`)
	if st["title"] != "T" {
		t.Fatalf("title: %+v", st)
	}
	if _, ok := st["runId"]; ok {
		t.Fatal("runId leaked")
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), "run-1") || strings.Contains(string(raw), `"env"`) {
		t.Fatalf("structured leak: %s", raw)
	}
}
