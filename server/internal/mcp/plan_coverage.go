package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	s "github.com/cocofhu/grasp/internal/mcp/structured"
)

// PlanCoverageOK checks test_result.json plan_coverage against plan.json leaves.
//
// Rules (aligned with clarified plan-fit gate):
//   - no plan / no leaves → pass (fail-open)
//   - with leaves: require plan_coverage covering every leaf exactly once,
//     each with passed=true, non-empty/non-whitespace evidence and at least one
//     referenced case in `cases`
//   - every referenced case must exist in cases[] and have status=passed, so a
//     leaf can't be marked passed on the strength of skipped or failed checks
//   - unknown or duplicate plan_id → fail
//
// Distinguishable reason strings help agents repair on exits.fail → implement.
func PlanCoverageOK(testJSON, planJSON string) (bool, string) {
	leaves := PlanLeafIDs(planJSON)
	if len(leaves) == 0 {
		return true, ""
	}

	var doc struct {
		Cases []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"cases"`
		PlanCoverage []struct {
			PlanID   string   `json:"plan_id"`
			Passed   bool     `json:"passed"`
			Evidence string   `json:"evidence"`
			Cases    []string `json:"cases"`
		} `json:"plan_coverage"`
	}
	if json.Unmarshal([]byte(testJSON), &doc) != nil {
		return false, "计划贴合度校验失败:无法解析 test_result.json"
	}
	if len(doc.PlanCoverage) == 0 {
		return false, "计划贴合度校验失败:缺少 plan_coverage(有计划叶子时必填)"
	}

	caseStatus := make(map[string]string, len(doc.Cases))
	for _, c := range doc.Cases {
		if name := strings.TrimSpace(c.Name); name != "" {
			caseStatus[name] = s.NormTestStatus(c.Status)
		}
	}
	leafSet := make(map[string]struct{}, len(leaves))
	for _, id := range leaves {
		leafSet[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(doc.PlanCoverage))
	for _, item := range doc.PlanCoverage {
		id := strings.TrimSpace(item.PlanID)
		if id == "" {
			return false, "计划贴合度校验失败:plan_coverage 存在空 plan_id"
		}
		if _, ok := leafSet[id]; !ok {
			return false, fmt.Sprintf("计划贴合度校验失败:未知 plan_id %s", id)
		}
		if _, dup := seen[id]; dup {
			return false, fmt.Sprintf("计划贴合度校验失败:重复 plan_id %s", id)
		}
		seen[id] = struct{}{}
		if !item.Passed {
			return false, fmt.Sprintf("计划贴合度校验失败:%s 未通过(passed≠true)", id)
		}
		if strings.TrimSpace(item.Evidence) == "" {
			return false, fmt.Sprintf("计划贴合度校验失败:%s 的 evidence 为空", id)
		}
		if reason := coverageCasesProblem(id, item.Cases, caseStatus); reason != "" {
			return false, reason
		}
	}

	var missing []string
	for _, id := range leaves {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return false, fmt.Sprintf("计划贴合度校验失败:未覆盖计划叶子 %s", strings.Join(missing, ", "))
	}
	return true, ""
}

// coverageCasesProblem returns the gate failure reason for one plan leaf's
// referenced cases, or "" when every reference exists and passed.
func coverageCasesProblem(id string, refs []string, caseStatus map[string]string) string {
	n := 0
	for _, ref := range refs {
		name := strings.TrimSpace(ref)
		if name == "" {
			continue
		}
		n++
		st, ok := caseStatus[name]
		if !ok {
			return fmt.Sprintf("计划贴合度校验失败:%s 关联的用例「%s」不存在于 cases", id, name)
		}
		if st != "passed" {
			return fmt.Sprintf("计划贴合度校验失败:%s 关联的用例「%s」未通过(%s)", id, name, st)
		}
	}
	if n == 0 {
		return fmt.Sprintf("计划贴合度校验失败:%s 未关联任何用例(plan_coverage.cases 必填)", id)
	}
	return ""
}
