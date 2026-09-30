package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestPreviewNodePromptExtrasLive(t *testing.T) {
	cases := []struct {
		name     string
		nodeType string
		cfg      map[string]any
		want     bool
	}{
		{"app_preview live", "app_preview", map[string]any{"direct_preview": true, "live_variants": true}, true},
		{"live default on", "app_preview", map[string]any{"direct_preview": true}, true},
		{"live off", "app_preview", map[string]any{"direct_preview": true, "live_variants": false}, false},
		{"not direct", "app_preview", map[string]any{"live_variants": true}, false},
		{"grasp never", "grasp", map[string]any{"direct_preview": true, "live_variants": true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := NodeReq{NodeType: c.nodeType, Config: c.cfg}
			got := previewNodePromptExtras(req)
			if strings.Contains(got, models.DefaultPreviewLiveContract) != c.want {
				t.Errorf("live contract present=%v, want %v", !c.want, c.want)
			}
			skills := liveVariantSkills(req)
			if (len(skills) == 1 && skills[0] == liveVariantSkillDir) != c.want {
				t.Errorf("skills=%v, want live=%v", skills, c.want)
			}
		})
	}
}

func TestPreviewNodePromptExtrasManualKeepsPageControl(t *testing.T) {
	got := previewNodePromptExtras(NodeReq{NodeType: "app_preview", Config: map[string]any{
		"direct_preview": true, "auto_inject": false, "live_variants": "true",
	}})
	for _, part := range []string{models.DefaultPreviewDirectManualContract, models.DefaultPreviewPageControlContract, models.DefaultPreviewLiveContract} {
		if !strings.Contains(got, part) {
			t.Errorf("missing contract part %.40q", part)
		}
	}
}
