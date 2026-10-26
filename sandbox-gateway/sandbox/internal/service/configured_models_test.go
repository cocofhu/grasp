package service

import (
	"testing"
)

func TestProjectConfiguredModelsKeepsOrderAndMarksMissing(t *testing.T) {
	spec := []configuredModelSpec{{ID: "", Vendor: "cursor"}, {ID: "aux", Vendor: "cursor"}, {ID: "gone", Vendor: "xai"}}
	catalog := []AgentModel{{ID: "aux", Name: "Auxiliary", Vendor: "xai"}, {ID: "other", Name: "Other"}}
	got := projectConfiguredModels(spec, catalog, nil)
	if len(got) != 3 || got[0].ID != "" || got[0].Name != "自动" || got[0].Unavailable {
		t.Fatalf("primary auto: %+v", got)
	}
	if got[1].ID != "aux" || got[1].Name != "Auxiliary" || got[1].Vendor != "xai" || got[1].Unavailable {
		t.Fatalf("known auxiliary: %+v", got[1])
	}
	if got[2].ID != "gone" || !got[2].Unavailable || got[2].Vendor != "xai" {
		t.Fatalf("missing model: %+v", got[2])
	}
	unknown := projectConfiguredModels([]configuredModelSpec{{ID: "only"}}, nil, errCatalog)
	if len(unknown) != 1 || unknown[0].Unavailable {
		t.Fatalf("catalog failure should not mark the saved model unavailable: %+v", unknown)
	}
}

var errCatalog = errString("catalog failed")

type errString string

func (e errString) Error() string { return string(e) }
