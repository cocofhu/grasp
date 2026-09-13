package handlers_test

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestPackRunArtifacts(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	pngMagic := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	b64 := base64.StdEncoding.EncodeToString(pngMagic)

	h.db.Create(&models.Run{
		ID: "run-pack-1", WorkflowID: "wf1", WorkflowName: "WF", Status: "completed",
		Title: "给产物页加打包下载", StartedAt: now,
	})
	h.db.Create(&models.Artifact{
		ID: "art-pack-md", RunID: "run-pack-1", NodeID: "approve",
		Name: "plan.json", Kind: "json", Content: `{"ok":true}`, SizeBytes: 10, CreatedAt: now,
	})
	h.db.Create(&models.Artifact{
		ID: "art-pack-img", RunID: "run-pack-1", NodeID: "test",
		Name: "shot.png", Kind: "image", Content: b64, SizeBytes: len(b64), CreatedAt: now.Add(time.Second),
	})
	h.db.Create(&models.Artifact{
		ID: "art-pack-evil", RunID: "run-pack-1", NodeID: "approve",
		Name: "../escape/report.md", Kind: "markdown", Content: "safe", SizeBytes: 4, CreatedAt: now.Add(2 * time.Second),
	})

	t.Run("200 zip members match ByRun and images decoded", func(t *testing.T) {
		w := h.do("GET", "/api/runs/run-pack-1/artifacts/pack", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("pack: %d %s", w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/zip") {
			t.Fatalf("Content-Type = %q", ct)
		}
		cd := w.Header().Get("Content-Disposition")
		if !strings.Contains(cd, "artifacts.zip") {
			t.Fatalf("Content-Disposition = %q", cd)
		}
		if !strings.Contains(cd, "给产物页加打包下载") && !strings.Contains(cd, "%") {
			// UTF-8 filename* may percent-encode CJK; attachment must still be present.
			if !strings.Contains(cd, "attachment") {
				t.Fatalf("Content-Disposition missing attachment: %s", cd)
			}
		}

		zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil {
			t.Fatalf("zip: %v", err)
		}
		if len(zr.File) != 3 {
			t.Fatalf("zip members = %d, want 3", len(zr.File))
		}
		byName := map[string][]byte{}
		for _, f := range zr.File {
			if strings.Contains(f.Name, "..") {
				t.Fatalf("path traversal in zip member %q", f.Name)
			}
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", f.Name, err)
			}
			body, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("read %s: %v", f.Name, err)
			}
			byName[f.Name] = body
		}

		var foundPlan, foundPNG, foundEscape bool
		for name, body := range byName {
			if strings.HasSuffix(name, "plan.json") {
				foundPlan = true
				if string(body) != `{"ok":true}` {
					t.Fatalf("plan.json body = %q", body)
				}
				if !strings.HasPrefix(name, "给产物页加打包下载/") && !strings.Contains(name, "/") {
					t.Fatalf("expected run directory prefix, got %q", name)
				}
			}
			if strings.HasSuffix(name, "shot.png") {
				foundPNG = true
				if !bytes.Equal(body, pngMagic) {
					t.Fatalf("shot.png not decoded PNG magic: %v", body)
				}
			}
			if strings.Contains(name, "report.md") || strings.Contains(name, "escape") {
				foundEscape = true
				if string(body) != "safe" {
					t.Fatalf("escaped member body = %q", body)
				}
				if strings.Contains(name, "..") {
					t.Fatalf("evil path kept: %q", name)
				}
			}
		}
		if !foundPlan || !foundPNG || !foundEscape {
			t.Fatalf("missing members: plan=%v png=%v escape=%v names=%v", foundPlan, foundPNG, foundEscape, keys(byName))
		}
	})

	t.Run("missing run 404", func(t *testing.T) {
		w := h.do("GET", "/api/runs/ghost-run/artifacts/pack", nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("unauthenticated 401", func(t *testing.T) {
		w := h.doWithCookie("GET", "/api/runs/run-pack-1/artifacts/pack", nil, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty run still 200 zip", func(t *testing.T) {
		h.db.Create(&models.Run{
			ID: "run-pack-empty", WorkflowID: "wf1", Status: "completed", StartedAt: now,
		})
		w := h.do("GET", "/api/runs/run-pack-empty/artifacts/pack", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("empty pack: %d %s", w.Code, w.Body.String())
		}
		zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil {
			t.Fatalf("zip: %v", err)
		}
		if len(zr.File) != 0 {
			t.Fatalf("empty run zip members = %d", len(zr.File))
		}
	})
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
