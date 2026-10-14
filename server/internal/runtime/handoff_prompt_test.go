package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestHandoffPromptSections(t *testing.T) {
	if downstreamHandoffText(nil) != "" || upstreamHandoffText(nil) != "" {
		t.Fatal("no downstream / upstream must add nothing")
	}
	down := downstreamHandoffText([]models.HandoffTarget{{NodeID: "test_review", Label: "测试评审", Outlet: "success"}})
	for _, want := range []string{"## 下游与交接(强制)", "「测试评审」(test_review)", "handoff", "failed"} {
		if !strings.Contains(down, want) {
			t.Errorf("downstream section missing %q:\n%s", want, down)
		}
	}
	up := upstreamHandoffText(&models.UpstreamHandoff{NodeID: "impl", Label: "实现", Text: "分支 feat/x 已推送", Source: models.HandoffSourcePlatform, Status: "failed"})
	for _, want := range []string{"## 上游交接", "「实现」", "执行失败", "平台", "分支 feat/x 已推送"} {
		if !strings.Contains(up, want) {
			t.Errorf("upstream section missing %q:\n%s", want, up)
		}
	}
}
