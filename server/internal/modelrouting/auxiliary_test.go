package modelrouting

import "testing"

func TestNormalizeAuxiliaryModels(t *testing.T) {
	binding := Binding{ModelID: " primary ", AuxiliaryModelIDs: []string{" aux-a ", "aux-b"}}
	if err := NormalizeAuxiliaryModels(&binding); err != nil {
		t.Fatal(err)
	}
	if binding.ModelID != "primary" || len(binding.AuxiliaryModelIDs) != 2 || binding.AuxiliaryModelIDs[0] != "aux-a" || binding.AuxiliaryModelIDs[1] != "aux-b" {
		t.Fatalf("normalized: %+v", binding)
	}
	legacy := Binding{ModelID: "only"}
	if err := NormalizeAuxiliaryModels(&legacy); err != nil || legacy.AuxiliaryModelIDs != nil || !AcceptsConfiguredModel(legacy, "") || !AcceptsConfiguredModel(legacy, "only") || AcceptsConfiguredModel(legacy, "other") {
		t.Fatalf("legacy single model: %+v", legacy)
	}
	for _, bad := range []Binding{
		{ModelID: "a", AuxiliaryModelIDs: []string{"a"}},
		{ModelID: "a", AuxiliaryModelIDs: []string{"b", "b"}},
		{ModelID: "a", AuxiliaryModelIDs: []string{" "}},
		{AuxiliaryModelIDs: []string{"", "b"}},
	} {
		if err := NormalizeAuxiliaryModels(&bad); err == nil {
			t.Fatalf("accepted invalid auxiliary list: %+v", bad)
		}
	}
	auto := Binding{AuxiliaryModelIDs: []string{"aux"}}
	if err := NormalizeAuxiliaryModels(&auto); err != nil || auto.ModelID != "" || !AcceptsConfiguredModel(auto, "") || !AcceptsConfiguredModel(auto, "aux") {
		t.Fatalf("empty primary: %+v %v", auto, err)
	}
}
