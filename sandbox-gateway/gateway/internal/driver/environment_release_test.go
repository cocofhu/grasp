package driver

import "testing"

func validReleasePlanFixture() EnvironmentReleasePlan {
	p := EnvironmentReleasePlan{Version: EnvironmentReleaseVersion, Resource: "owned", Namespace: "sandbox", Objects: []EnvironmentReleaseObject{{Kind: "Deployment", Namespace: "sandbox", Name: "deployment", UID: "dep-original"}, {Kind: "PersistentVolumeClaim", Namespace: "sandbox", Name: "disk", UID: "pvc-original"}, {Kind: "PersistentVolume", Name: "physical", UID: "pv-original", ObserveOnly: true}}}
	p.SHA256 = p.Digest()
	return p
}
func TestEnvironmentReleasePlanCanonicalDigestAndClosedUIDScope(t *testing.T) {
	original := validReleasePlanFixture()
	if !original.Valid() {
		t.Fatal("valid physical plan rejected")
	}
	copy := original
	copy.Objects = append([]EnvironmentReleaseObject(nil), original.Objects...)
	copy.Objects[0], copy.Objects[2] = copy.Objects[2], copy.Objects[0]
	if copy.Digest() != original.SHA256 || original.Objects[0].Kind != "Deployment" {
		t.Fatal("canonical digest mutated or depends on order")
	}
	for _, test := range []struct {
		name   string
		mutate func(*EnvironmentReleasePlan)
	}{
		{"version", func(p *EnvironmentReleasePlan) { p.Version = "other" }}, {"resource", func(p *EnvironmentReleasePlan) { p.Resource = "../other" }}, {"namespace", func(p *EnvironmentReleasePlan) { p.Namespace = "" }}, {"missing-volume", func(p *EnvironmentReleasePlan) { p.Objects = p.Objects[:2] }}, {"missing-uid", func(p *EnvironmentReleasePlan) { p.Objects[0].UID = "" }}, {"unsafe-name", func(p *EnvironmentReleasePlan) { p.Objects[0].Name = "../host" }}, {"foreign-namespace", func(p *EnvironmentReleasePlan) { p.Objects[0].Namespace = "another" }}, {"skip-delete", func(p *EnvironmentReleasePlan) { p.Objects[0].ObserveOnly = true }}, {"direct-pv-delete", func(p *EnvironmentReleasePlan) { p.Objects[2].ObserveOnly = false }}, {"namespaced-pv", func(p *EnvironmentReleasePlan) { p.Objects[2].Namespace = "sandbox" }}, {"unsupported-kind", func(p *EnvironmentReleasePlan) { p.Objects[0].Kind = "Namespace" }}, {"duplicate", func(p *EnvironmentReleasePlan) { p.Objects = append(p.Objects, p.Objects[0]) }}, {"two-deployments", func(p *EnvironmentReleasePlan) {
			p.Objects = append(p.Objects, EnvironmentReleaseObject{Kind: "Deployment", Namespace: p.Namespace, Name: "other", UID: "other"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := original
			p.Objects = append([]EnvironmentReleaseObject(nil), original.Objects...)
			test.mutate(&p)
			p.SHA256 = p.Digest()
			if p.Valid() {
				t.Fatal("unsafe physical scope accepted")
			}
		})
	}
	original.SHA256 = "corrupted"
	if original.Valid() {
		t.Fatal("unbound digest accepted")
	}
}
