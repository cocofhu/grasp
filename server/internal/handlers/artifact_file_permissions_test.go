package handlers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
)

func TestImportedFilesDoNotExpandAnExistingPublicShare(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "file-share", "research1", true)
	created := parseJSON(t, h.do("POST", "/api/runs/file-share/nodes/research1/share-link", map[string]any{"ttlTier": "24h", "permissionPreset": "full"}))
	url := created["url"].(string)
	token := url[strings.Index(url, "#t=")+3:]
	a, err := h.h.Arts.SaveFile(context.Background(), "file-share", "research1", "private.txt", "", strings.NewReader("PRIVATE-IMPORTED-FILE"))
	if err != nil {
		t.Fatal(err)
	}
	list := h.doPublic("GET", "/public/approvals/artifacts", nil, map[string]string{headerShareToken: token})
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), a.ID) || strings.Contains(list.Body.String(), a.Name) {
		t.Fatalf("public list=%d %s", list.Code, list.Body)
	}
	content := h.doPublic("GET", "/public/approvals/artifacts/private.txt/content", nil, map[string]string{headerShareToken: token})
	if content.Code != http.StatusNotFound {
		t.Fatalf("public content=%d %s", content.Code, content.Body)
	}
	raw := h.doPublic("GET", "/api/artifacts/"+a.ID+"/raw", nil, map[string]string{headerShareToken: token})
	if raw.Code == http.StatusOK {
		t.Fatal("share token granted a private file")
	}
	raw = h.do("GET", "/api/artifacts/"+a.ID+"/raw?revision=1", nil)
	if raw.Code != http.StatusOK || raw.Body.String() != "PRIVATE-IMPORTED-FILE" {
		t.Fatalf("authenticated raw=%d %s", raw.Code, raw.Body)
	}
}

func TestFileZIPStreamsOriginalBytesAndRejectsMissingStorage(t *testing.T) {
	h := newHarness(t)
	if err := h.db.Create(&models.Run{ID: "binary-pack", Status: "completed", StartedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte{0, 255, 1, 0, 22, 44}
	a, err := h.h.Arts.SaveFile(context.Background(), "binary-pack", "writer", "data.bin", "", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	pack := h.do("GET", "/api/runs/binary-pack/artifacts/pack", nil)
	if pack.Code != 200 {
		t.Fatalf("pack=%d %s", pack.Code, pack.Body)
	}
	z, err := zip.NewReader(bytes.NewReader(pack.Body.Bytes()), int64(pack.Body.Len()))
	if err != nil || len(z.File) != 1 {
		t.Fatalf("zip=%v err=%v", z, err)
	}
	r, err := z.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("zipped=%v err=%v", got, err)
	}
	for _, revision := range []string{"0", "-1", "nope"} {
		if res := h.do("GET", "/api/artifacts/"+a.ID+"/raw?revision="+revision, nil); res.Code != 400 {
			t.Fatalf("invalid revision=%s status=%d", revision, res.Code)
		}
	}
	var ref string
	if err = h.db.Table("artifacts").Select("content").Where("id = ?", a.ID).Row().Scan(&ref); err != nil {
		t.Fatal(err)
	}
	store, _ := filestore.FromDB(h.db)
	f, err := store.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	if res := h.do("GET", "/api/artifacts/"+a.ID+"/raw", nil); res.Code != 503 {
		t.Fatalf("missing object=%d %s", res.Code, res.Body)
	}
	if res := h.do("GET", "/api/runs/binary-pack/artifacts/pack", nil); res.Code != 503 || strings.Contains(res.Body.String(), name) {
		t.Fatalf("missing ZIP member=%d %s", res.Code, res.Body)
	}
}
