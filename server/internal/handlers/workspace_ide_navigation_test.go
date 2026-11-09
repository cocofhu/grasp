package handlers

import (
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCanonicalIDEEntryNavigatesBrowserToTrustedFolderWithoutLoop(t *testing.T) {
	const folder = "/tmp/grasp-private-workspace/owned/restores/original-copy"
	for _, suffix := range []string{"", "?folder=/root/workspace&workspace=old.code-workspace&theme=dark", "?workspace=&theme=dark", "?folder=" + url.QueryEscape(folder) + "&folder=/other"} {
		t.Run(suffix, func(t *testing.T) {
			r := gin.New()
			requests := 0
			r.GET("/sandbox/:id/*path", func(c *gin.Context) {
				requests++
				if redirectCanonicalIDEEntry(c, 22, folder) {
					return
				}
				c.String(200, "current-root")
			})
			srv := httptest.NewServer(r)
			defer srv.Close()
			response, err := srv.Client().Get(srv.URL + "/sandbox/22/" + suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			q := response.Request.URL.Query()
			if response.StatusCode != 200 || string(body) != "current-root" || requests != 2 || q.Get("folder") != folder || len(q["folder"]) != 1 || q.Has("workspace") {
				t.Fatalf("browser did not reach one canonical navigation: requests=%d url=%s", requests, response.Request.URL)
			}
			if strings.Contains(suffix, "theme=dark") && q.Get("theme") != "dark" {
				t.Fatal("unrelated browser setting lost")
			}
		})
	}
}

func TestCanonicalIDENavigationSkipsAssetsWebSocketsAndExistingCanonicalURL(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, folder, query string
		websocket                         bool
	}{
		{name: "already-current", method: "GET", path: "/", folder: "/current", query: "folder=%2Fcurrent"},
		{name: "asset", method: "GET", path: "/static/workbench.js", folder: "/current"},
		{name: "remote-files", method: "GET", path: "/vscode-remote-resource", folder: "/current"},
		{name: "websocket", method: "GET", path: "/", folder: "/current", websocket: true},
		{name: "post", method: "POST", path: "/", folder: "/current"},
		{name: "no-grant-folder", method: "GET", path: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tc.method, "http://platform/sandbox/22/"+"?"+tc.query, nil)
			c.Params = gin.Params{{Key: "path", Value: tc.path}}
			if tc.websocket {
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
			}
			if redirectCanonicalIDEEntry(c, 22, tc.folder) || w.Header().Get("Location") != "" {
				t.Fatal("non-navigation or current folder was redirected")
			}
		})
	}
}
