package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func releaseWireFixture(resource string) EnvironmentReleaseView {
	p := EnvironmentReleasePlan{Version: EnvironmentReleaseVersion, Resource: resource, Namespace: "owned", Objects: []EnvironmentReleaseObject{{Kind: "Deployment", Namespace: "owned", Name: "d", UID: "deployment"}, {Kind: "PersistentVolumeClaim", Namespace: "owned", Name: "p", UID: "claim"}, {Kind: "PersistentVolume", Name: "volume", UID: "physical", ObserveOnly: true}}}
	p.SHA256 = p.Digest()
	return EnvironmentReleaseView{ID: "original-release", State: "prepared", Plan: p}
}
func TestEnvironmentReleaseClientUsesBoundGatewayAndRejectsWrongPhysicalResource(t *testing.T) {
	wrong := false
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer synthetic-key" {
			t.Error("missing scoped gateway auth")
		}
		v := releaseWireFixture("native-id")
		if wrong {
			v.Plan.Resource = "different-environment"
			v.Plan.SHA256 = v.Plan.Digest()
		}
		_ = json.NewEncoder(w).Encode(v)
	}))
	defer server.Close()
	g := NewGatewayClient("http://unreachable.invalid", "")
	g.gateways = map[string]*GatewayClient{"k8s": NewGatewayClient(server.URL, "synthetic-key")}
	id := qualifiedGatewayID("k8s", "native-id")
	if _, err := g.PreviewEnvironmentRelease(context.Background(), id, EnvironmentReleasePrepare{ID: "original-release", Lineage: "lineage"}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/api/v1/sandboxes/native-id/releases" {
		t.Fatal(paths)
	}
	wrong = true
	if _, err := g.GetEnvironmentRelease(context.Background(), id, "original-release"); err == nil {
		t.Fatal("wrong-resource physical proof accepted")
	}
	if strings.Contains(paths[1], id) {
		t.Fatal("qualified identifier escaped into native path")
	}
}
func TestEnvironmentReleasePlanRequiresUIDClosedWholeEnvironmentScope(t *testing.T) {
	valid := releaseWireFixture("native-id").Plan
	if !valid.Valid() {
		t.Fatal("valid scope rejected")
	}
	for _, mutate := range []func(*EnvironmentReleasePlan){func(p *EnvironmentReleasePlan) { p.Objects = p.Objects[:2] }, func(p *EnvironmentReleasePlan) { p.Objects[0].UID = "" }, func(p *EnvironmentReleasePlan) { p.Objects[0].Kind = "Namespace" }, func(p *EnvironmentReleasePlan) { p.Objects[0].Namespace = "another" }, func(p *EnvironmentReleasePlan) { p.Objects[2].ObserveOnly = false }, func(p *EnvironmentReleasePlan) { p.Objects = append(p.Objects, p.Objects[0]) }, func(p *EnvironmentReleasePlan) { p.Resource = "../other" }} {
		p := valid
		p.Objects = append([]EnvironmentReleaseObject(nil), valid.Objects...)
		mutate(&p)
		p.SHA256 = p.Digest()
		if p.Valid() {
			t.Fatal("invalid physical identity accepted", p)
		}
	}
}
