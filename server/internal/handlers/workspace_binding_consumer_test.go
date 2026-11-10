package handlers

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCanonicalIDEFolderCannotBeOverriddenByBrowserWorkspaceQuery(t *testing.T) {
	request := httptest.NewRequest("GET", "http://native/?folder=/original&workspace=old.code-workspace&theme=dark", nil)
	browser := url.Values{"folder": {"/browser-selected"}, "workspace": {"arbitrary.code-workspace"}, "theme": {"light"}}
	mergeCanonicalUpstreamQuery(request, browser, "/trusted/restores/original-job")
	query := request.URL.Query()
	if query.Get("folder") != "/trusted/restores/original-job" || query.Get("workspace") != "" || query.Get("theme") != "light" {
		t.Fatal(query)
	}
}
