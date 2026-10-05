package nodereg

import (
	"fmt"

	"github.com/cocofhu/grasp/internal/mcp"
)

// TestVerdict fails when any case failed, test_result.json is malformed, or a
// non-empty plan is not fully covered by passed plan_coverage evidence.
func TestVerdict(content, planJSON string) (bool, string) {
	n := mcp.TestFailedCount(content)
	if n < 0 {
		return false, "测试结果解析失败:无法读取 test_result.json"
	}
	if n > 0 {
		return false, fmt.Sprintf("测试未通过:%d 个用例失败,需修复后重新测试", n)
	}
	if ok, reason := mcp.PlanCoverageOK(content, planJSON); !ok {
		return false, reason
	}
	return true, ""
}

// ReviewVerdict fails on request_changes / reject or a malformed review.json.
func ReviewVerdict(content string) (bool, string) {
	verdict, ok := mcp.ReviewVerdictOK(content)
	if !ok {
		return false, "评审结果无效:无法读取 review.json 或 verdict 不合法"
	}
	switch verdict {
	case "request_changes":
		return false, "评审结论为 request_changes:需按评审意见修改后重新评审"
	case "reject":
		return false, "评审结论为 reject:方案/实现被否决,需整改后重新评审"
	default:
		return true, ""
	}
}
