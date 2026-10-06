package structured

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAndRenderImplementationResult(t *testing.T) {
	args := map[string]any{
		"summary":     "实现完成",
		"change_type": "feature",
		"changed_areas": []map[string]any{
			{"title": "api", "detail": "新增端点"},
			{"title": ""}, // dropped (empty title)
		},
		"tests":            []string{"go test ok"},
		"breaking_changes": []string{"none"},
		"follow_ups":       []string{"docs"},
	}
	doc, err := ParseImplementationResult(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.ChangedAreas) != 1 {
		t.Fatalf("changed areas: %d", len(doc.ChangedAreas))
	}
	// Missing summary -> error.
	if _, err := ParseImplementationResult(map[string]any{}); err == nil {
		t.Fatal("empty summary should error")
	}
	// Decode error (wrong shape for changed_areas).
	if _, err := ParseImplementationResult(map[string]any{"summary": "s", "changed_areas": "not-a-list"}); err == nil {
		t.Fatal("bad changed_areas should error")
	}

	md := RenderImplementationResultMarkdown(`{"summary":"done","change_type":"feat","changed_areas":[{"title":"a","detail":"d"},{"title":"b"}],"tests":["t"],"breaking_changes":["bc"],"follow_ups":["f"]}`)
	for _, want := range []string{"feat", "改动", "测试", "破坏性变更", "后续", "**a**", "**b**"} {
		if !strings.Contains(md, want) {
			t.Fatalf("impl md missing %q: %s", want, md)
		}
	}
	if RenderImplementationResultMarkdown(`{bad`) != `{bad` {
		t.Fatal("bad json returns raw")
	}
}

func TestParseAndRenderTestResult(t *testing.T) {
	args := map[string]any{
		"summary": "测试总结",
		"cases": []map[string]any{
			{"id": "c1", "name": "login", "status": "pass"},
			{"id": "c2", "name": "logout", "status": "fail", "detail": "boom"},
			{"name": "", "status": "skip"}, // no name -> kept? craft to exercise
		},
		"defects": []map[string]any{
			{"id": "d1", "title": "crash", "severity": "critical", "status": "open"},
		},
		"variances":  "小偏差",
		"assessment": "可接受",
		"passed":     1, "failed": 1, "skipped": 1,
	}
	doc, err := ParseTestResult(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Summary == "" {
		t.Fatal("summary lost")
	}
	if _, err := ParseTestResult(map[string]any{}); err == nil {
		t.Fatal("empty summary should error")
	}

	md := RenderTestResultMarkdown(`{"summary":"s","cases":[{"id":"c1","name":"login","status":"passed","detail":"ok"},{"id":"c2","name":"logout","status":"failed"}],"defects":[{"id":"d1","title":"crash","severity":"high","detail":"x","status":"open"}],"variances":"v","assessment":"a","passed":1,"failed":1,"skipped":0}`)
	for _, want := range []string{"login", "logout", "crash", "v", "a"} {
		if !strings.Contains(md, want) {
			t.Fatalf("test md missing %q: %s", want, md)
		}
	}
	if RenderTestResultMarkdown(`{bad`) != `{bad` {
		t.Fatal("bad json raw")
	}
	if TestFailedCount(`{bad`) != -1 {
		t.Error("bad json should return -1")
	}
	if TestFailedCount(`{"summary":"s","failed":2,"cases":[{"name":"a","status":"failed"},{"name":"b","status":"failed"}]}`) != 2 {
		t.Error("failed count")
	}
	if TestSkippedCount(`{bad`) != -1 {
		t.Error("bad json should return -1 for skipped")
	}
	if TestSkippedCount(`{"summary":"s","skipped":2,"cases":[{"name":"a","status":"skipped"},{"name":"b","status":"skipped"}]}`) != 2 {
		t.Error("skipped count")
	}
}

func TestParseTestResultPlanCoverage(t *testing.T) {
	doc, err := ParseTestResult(map[string]any{
		"summary": "s",
		"plan_coverage": []map[string]any{
			{"plan_id": " g1.1 ", "title": " 叶子 ", "passed": true, "evidence": " 做了 A "},
			{"plan_id": "", "passed": true, "evidence": "drop me"},
			{"plan_id": "g1.2", "passed": false, "evidence": "  "},
		},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.PlanCoverage) != 2 {
		t.Fatalf("plan_coverage len=%d, want 2 (empty plan_id dropped)", len(doc.PlanCoverage))
	}
	if doc.PlanCoverage[0].PlanID != "g1.1" || doc.PlanCoverage[0].Title != "叶子" ||
		!doc.PlanCoverage[0].Passed || doc.PlanCoverage[0].Evidence != "做了 A" {
		t.Fatalf("first item not normalized: %+v", doc.PlanCoverage[0])
	}
	if doc.PlanCoverage[1].PlanID != "g1.2" || doc.PlanCoverage[1].Passed || doc.PlanCoverage[1].Evidence != "" {
		t.Fatalf("second item not normalized: %+v", doc.PlanCoverage[1])
	}
	md := RenderTestResultMarkdown(`{"summary":"s","plan_coverage":[{"plan_id":"g1.1","title":"x","passed":true,"evidence":"ok"}]}`)
	if !strings.Contains(md, "计划贴合度") || !strings.Contains(md, "g1.1") {
		t.Fatalf("md missing plan coverage: %s", md)
	}
}

func TestParseTestResultScreenshots(t *testing.T) {
	// Only artifact references are accepted: inline data is ignored/dropped,
	// entries without an artifact drop, captions are trimmed, count is capped.
	shots := []map[string]any{
		{"artifact": " shot-1.png ", "caption": " 首页 "}, // trimmed
		{"caption": "empty"}, // no artifact -> dropped
	}
	// pad well past the cap to prove truncation to maxTestScreenshots
	for i := 0; i < 20; i++ {
		shots = append(shots, map[string]any{"artifact": "z.png"})
	}
	doc, err := ParseTestResult(map[string]any{"summary": "s", "screenshots": shots})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Screenshots) != maxTestScreenshots {
		t.Fatalf("screenshots = %d, want cap %d", len(doc.Screenshots), maxTestScreenshots)
	}
	if doc.Screenshots[0].Artifact != "shot-1.png" {
		t.Errorf("artifact not preserved/trimmed: %+v", doc.Screenshots[0])
	}
	if doc.Screenshots[0].Caption != "首页" {
		t.Errorf("caption not trimmed: %q", doc.Screenshots[0].Caption)
	}

	// no screenshots -> nil, markdown has no screenshot section
	mdNone := RenderTestResultMarkdown(`{"summary":"s"}`)
	if strings.Contains(mdNone, "测试截图") {
		t.Errorf("markdown should omit screenshot section when none: %s", mdNone)
	}

	// with screenshots -> section present, captions listed, no base64 inlined
	b, _ := json.Marshal(doc)
	md := RenderTestResultMarkdown(string(b))
	if !strings.Contains(md, "测试截图(10 张)") {
		t.Errorf("markdown missing screenshot section: %s", md)
	}
	if !strings.Contains(md, "首页") {
		t.Errorf("markdown missing caption: %s", md)
	}
}

func TestParseTestResultKeepsScreenshotMetadata(t *testing.T) {
	doc, err := ParseTestResult(map[string]any{
		"summary": "s",
		"screenshots": []map[string]any{
			{"mimeType": "image/png"}, // no artifact -> dropped
			{"artifact": "shot.png", "caption": "home", "mimeType": "image/webp"},
		},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Screenshots) != 1 {
		t.Fatalf("screenshots = %d, want 1", len(doc.Screenshots))
	}
	s := doc.Screenshots[0]
	if s.Artifact != "shot.png" || s.Caption != "home" || s.MimeType != "image/webp" {
		t.Errorf("metadata not preserved: %+v", s)
	}
}

func TestValidateScreenshotArtifacts(t *testing.T) {
	doc, err := ParseTestResult(map[string]any{
		"summary": "s",
		"screenshots": []map[string]any{
			{"artifact": "shot.jpg", "caption": "home"},
			{"artifact": "missing.png"},
		},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	store := map[string]bool{"shot.jpg": true}
	valErr := doc.ValidateScreenshotArtifacts(func(name string) bool {
		return store[name]
	})
	if valErr == nil {
		t.Fatal("expected error for missing.png")
	}
	if !strings.Contains(valErr.Error(), "missing.png") {
		t.Errorf("error = %q, want missing artifact name", valErr)
	}
	doc2, _ := ParseTestResult(map[string]any{
		"summary":     "s",
		"screenshots": []map[string]any{{"artifact": "shot.jpg"}},
	})
	if err := doc2.ValidateScreenshotArtifacts(func(name string) bool { return store[name] }); err != nil {
		t.Errorf("all exist: %v", err)
	}
}

func TestParseAndRenderClarifiedRequirement(t *testing.T) {
	args := map[string]any{
		"title":        "登录",
		"summary":      "整体概述",
		"background":   "需要安全入口",
		"goals":        []string{"完成登录"},
		"in_scope":     []string{"邮箱登录"},
		"out_of_scope": []string{"注册"},
		"user_scenarios": []map[string]any{
			{"name": "首次登录", "actor": "用户", "trigger": "打开登录页", "flow": "输入邮箱验证码", "outcome": "进入首页"},
		},
		"functional_requirements": []map[string]any{
			{"id": "f1", "title": "登录", "detail": "用户登录", "acceptance_criteria": []string{"密码校验"}, "scenario_ids": []string{"s1", "missing"}},
			{"title": ""}, // dropped
		},
		"non_functional_requirements": []map[string]any{
			{"id": "n1", "category": "performance", "detail": "<200ms", "metric": "p95 < 200ms"},
		},
		"assumptions":    []string{"用户有邮箱"},
		"dependencies":   []string{"邮件服务"},
		"constraints":    []string{"仅内网"},
		"open_questions": []string{"验证码?"},
	}
	doc, err := ParseClarifiedRequirement(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.FunctionalRequirements) != 1 {
		t.Fatalf("func reqs: %d", len(doc.FunctionalRequirements))
	}
	if doc.FunctionalRequirements[0].Priority != "must" {
		t.Fatalf("default priority: %q", doc.FunctionalRequirements[0].Priority)
	}
	if len(doc.FunctionalRequirements[0].ScenarioIDs) != 1 || doc.FunctionalRequirements[0].ScenarioIDs[0] != "s1" {
		t.Fatalf("scenario_ids filter: %+v", doc.FunctionalRequirements[0].ScenarioIDs)
	}
	if _, err := ParseClarifiedRequirement(map[string]any{}); err == nil {
		t.Fatal("empty summary should error")
	}
	if _, err := ParseClarifiedRequirement(map[string]any{
		"title": "t", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"functional_requirements": []map[string]any{{"title": "f", "detail": "d"}},
		"assumptions":             []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
	}); err == nil {
		t.Fatal("missing acceptance_criteria should error")
	}
	if _, err := ParseClarifiedRequirement(map[string]any{
		"title": "t", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"functional_requirements": []map[string]any{
			{"title": "f", "detail": "d", "acceptance_criteria": []string{"ac"}, "priority": "maybe"},
		},
		"assumptions": []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
	}); err == nil {
		t.Fatal("invalid priority should error")
	}
	thin := map[string]any{"summary": "s", "functional_requirements": []map[string]any{{"title": "f"}}}
	if _, err := ParseClarifiedRequirement(thin); err == nil {
		t.Fatal("thin payload should error")
	}

	md := RenderClarifiedRequirementMarkdown(`{
		"title":"T","summary":"s","background":"bg","goals":["g1"],"in_scope":["in"],
		"functional_requirements":[{"id":"f1","title":"登录","detail":"d","priority":"must","acceptance_criteria":["ac"]}],
		"non_functional_requirements":[{"id":"n1","category":"performance","detail":"fast","metric":"p95"}],
		"assumptions":["a"],"dependencies":["dep"],"constraints":["c"],"out_of_scope":["oos"],"open_questions":["q"]
	}`)
	for _, want := range []string{"概述", "背景", "目标", "范围内", "功能需求", "假设", "依赖", "约束", "登录", "performance", "p95", "c", "oos", "q"} {
		if !strings.Contains(md, want) {
			t.Fatalf("clarified md missing %q: %s", want, md)
		}
	}
	if RenderClarifiedRequirementMarkdown(`{bad`) != `{bad` {
		t.Fatal("bad json raw")
	}

	var fixtureArgs map[string]any
	if err := json.Unmarshal([]byte(MinimalValidClarifiedRequirementJSON), &fixtureArgs); err != nil {
		t.Fatalf("fixture json: %v", err)
	}
	if _, err := ParseClarifiedRequirement(fixtureArgs); err != nil {
		t.Fatalf("minimal fixture should parse: %v", err)
	}
}

func TestParseClarifiedRequirementRichAndRender(t *testing.T) {
	base := map[string]any{
		"title": " ", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"assumptions": []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
		"functional_requirements": []map[string]any{
			{"title": "f", "detail": "d", "acceptance_criteria": []string{"ac"}},
		},
	}
	for _, miss := range []string{"title", "summary", "background"} {
		args := cloneMap(base)
		args[miss] = "   "
		if _, err := ParseClarifiedRequirement(args); err == nil {
			t.Fatalf("blank %s should error", miss)
		}
	}
	for _, list := range []string{"goals", "in_scope", "out_of_scope", "assumptions", "dependencies", "constraints"} {
		args := cloneMap(base)
		args[list] = []string{}
		if _, err := ParseClarifiedRequirement(args); err == nil {
			t.Fatalf("empty %s should error", list)
		}
	}
	if _, err := ParseClarifiedRequirement(map[string]any{
		"title": "t", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"assumptions": []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
		"functional_requirements": []map[string]any{{"title": "f", "detail": "", "acceptance_criteria": []string{"ac"}}},
	}); err == nil {
		t.Fatal("empty FR detail should error")
	}
	if _, err := ParseClarifiedRequirement(map[string]any{
		"title": "t", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"assumptions": []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
		"functional_requirements": []map[string]any{{"title": ""}},
	}); err == nil {
		t.Fatal("no FR should error")
	}
	if _, err := ParseClarifiedRequirement(map[string]any{
		"title": "t", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"assumptions": []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
		"functional_requirements": []map[string]any{
			{"title": "f", "detail": "d", "acceptance_criteria": []string{"ac"}},
		},
		"non_functional_requirements": []map[string]any{
			{"detail": "x", "category": "not-a-cat"},
		},
	}); err == nil {
		t.Fatal("bad nfr category should error")
	}
	if _, err := ParseClarifiedRequirement(map[string]any{
		"title": "t", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"assumptions": []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
		"functional_requirements": []map[string]any{
			{"title": "f", "detail": "d", "acceptance_criteria": []string{"ac"}},
		},
		"external_interfaces": []map[string]any{{"name": "API", "kind": "weird"}},
	}); err == nil {
		t.Fatal("bad iface kind should error")
	}
	if _, err := ParseClarifiedRequirement(map[string]any{
		"title": "t", "summary": "s", "background": "b",
		"goals": []string{"g"}, "in_scope": []string{"i"}, "out_of_scope": []string{"o"},
		"assumptions": []string{"a"}, "dependencies": []string{"d"}, "constraints": []string{"c"},
		"functional_requirements": []map[string]any{
			{"title": "f", "detail": "d", "acceptance_criteria": []string{"ac"}},
		},
		"external_interfaces": []map[string]any{{"name": "API", "kind": "system", "direction": "sideways"}},
	}); err == nil {
		t.Fatal("bad iface direction should error")
	}

	rich := map[string]any{
		"title": "登录", "summary": "概述", "background": "背景",
		"goals": []string{"完成登录"}, "success_metrics": []string{"转化率"},
		"in_scope": []string{"邮箱"}, "out_of_scope": []string{"OAuth"},
		"personas": []map[string]any{
			{"name": "", "description": "drop"},
			{"name": "运营", "description": "日常操作", "goals": []string{"快速登录"}},
		},
		"user_scenarios": []map[string]any{
			{"name": ""},
			{"name": "首次", "actor": "用户", "trigger": "打开页", "flow": "输码", "outcome": "进首页"},
		},
		"functional_requirements": []map[string]any{
			{
				"title": "验证码登录", "detail": "邮箱+验证码", "priority": "should",
				"acceptance_criteria": []string{"5 分钟有效"}, "scenario_ids": []string{"s1"},
			},
		},
		"non_functional_requirements": []map[string]any{
			{"detail": ""},
			{"detail": "可用", "category": ""},
			{"detail": "安全", "category": "security", "metric": "无明文"},
		},
		"external_interfaces": []map[string]any{
			{"name": ""},
			{"name": "邮件网关", "kind": "", "direction": "", "description": "发验证码"},
			{"name": "审计", "kind": "system", "direction": "out", "description": "写日志"},
		},
		"data_entities": []map[string]any{
			{"name": ""},
			{"name": "验证码", "description": "临时凭证", "attributes": []string{"code", "expires_at"}},
		},
		"business_rules": []string{"单设备"}, "edge_cases": []string{"过期码"},
		"assumptions": []string{"有邮箱"}, "dependencies": []string{"SMTP"}, "constraints": []string{"内网"},
		"limitations": []string{"无生物识别"},
		"risks": []map[string]any{
			{"description": ""},
			{"description": "短信被拦", "mitigation": "降级邮件"},
		},
		"glossary": []map[string]any{
			{"term": "", "definition": "x"},
			{"term": "OTP", "definition": "一次性密码"},
		},
		"open_questions": []string{"是否支持短信?"},
	}
	doc, err := ParseClarifiedRequirement(rich)
	if err != nil {
		t.Fatalf("rich parse: %v", err)
	}
	if len(doc.Personas) != 1 || doc.Personas[0].ID != "u1" {
		t.Fatalf("personas: %+v", doc.Personas)
	}
	if len(doc.UserScenarios) != 1 || doc.UserScenarios[0].ID != "s1" {
		t.Fatalf("scenarios: %+v", doc.UserScenarios)
	}
	if len(doc.ExternalInterfaces) != 2 || doc.ExternalInterfaces[0].Kind != "system" || doc.ExternalInterfaces[0].Direction != "both" {
		t.Fatalf("ifaces: %+v", doc.ExternalInterfaces)
	}
	if len(doc.DataEntities) != 1 || len(doc.Risks) != 1 || len(doc.Glossary) != 1 {
		t.Fatalf("entities/risks/glossary: %+v %+v %+v", doc.DataEntities, doc.Risks, doc.Glossary)
	}
	if len(doc.NonFunctionalRequirements) != 2 || doc.NonFunctionalRequirements[0].Category != "other" {
		t.Fatalf("nfr: %+v", doc.NonFunctionalRequirements)
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	md := RenderClarifiedRequirementMarkdown(string(raw))
	for _, want := range []string{
		"用户画像", "运营", "用户场景", "首次", "角色:", "触发:", "流程:", "结果:",
		"外部接口", "邮件网关", "数据实体", "验证码", "业务规则", "边界与异常",
		"限制", "风险", "缓解:", "术语表", "OTP", "成功指标", "[场景]", "[验收]",
		"security", "〔指标:",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("rich md missing %q:\n%s", want, md)
		}
	}
	// NFR without category branch in render
	md2 := RenderClarifiedRequirementMarkdown(`{"summary":"s","non_functional_requirements":[{"detail":"plain"}]}`)
	if !strings.Contains(md2, "- plain") {
		t.Fatalf("nfr no-category render: %s", md2)
	}
	if qs := ClarifiedOpenQuestions(string(raw)); len(qs) != 1 {
		t.Fatalf("open questions: %v", qs)
	}
	if ClarifiedOpenQuestions(`{bad`) != nil {
		t.Fatal("bad json open questions")
	}
	if ClarifiedOpenQuestions(`{"summary":"s","open_questions":["  "]}`) != nil {
		t.Fatal("blank open questions should be nil")
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestParseAndRenderReview(t *testing.T) {
	args := map[string]any{
		"summary": "整体不错",
		"verdict": "APPROVE_WITH_COMMENTS",
		"findings": []map[string]any{
			{"title": "空指针", "severity": "critical", "file": "a.go", "line": 12, "detail": "d", "suggestion": "加判空"},
			{"title": "命名", "severity": "low", "file": "b.go"},
			{"title": ""}, // dropped
		},
		"action_items": []string{"补测试", "更新文档"},
	}
	doc, err := ParseReview(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Findings) != 2 || doc.Findings[0].ID != "v1" {
		t.Fatalf("findings: %+v", doc.Findings)
	}
	// critical sorts before low.
	if doc.Findings[0].Severity != "critical" {
		t.Errorf("severity sort: %+v", doc.Findings)
	}
	// Empty summary / invalid verdict errors.
	if _, err := ParseReview(map[string]any{"verdict": "approve"}); err == nil {
		t.Error("empty summary should error")
	}
	if _, err := ParseReview(map[string]any{"summary": "s", "verdict": "maybe"}); err == nil {
		t.Error("invalid verdict should error")
	}

	md := RenderReviewMarkdown(`{"summary":"s","verdict":"reject","findings":[{"id":"v1","title":"bug","severity":"high","file":"x.go","line":5,"detail":"d","suggestion":"fix"}],"action_items":["do it"]}`)
	for _, want := range []string{"reject", "评审意见", "bug", "x.go:5", "建议", "待处理项", "do it"} {
		if !strings.Contains(md, want) {
			t.Fatalf("review md missing %q: %s", want, md)
		}
	}
	if RenderReviewMarkdown(`{bad`) != `{bad` {
		t.Error("bad json raw")
	}

	// ReviewVerdict extraction.
	if ReviewVerdict(`{"summary":"s","verdict":"approve"}`) != "approve" {
		t.Error("verdict extract")
	}
	if ReviewVerdict(`{"verdict":"bogus"}`) != "" {
		t.Error("invalid verdict -> empty")
	}
	if ReviewVerdict(`{bad`) != "" {
		t.Error("bad json -> empty")
	}
	if _, ok := ReviewVerdictOK(`{"summary":"s","verdict":"approve"}`); !ok {
		t.Error("ReviewVerdictOK approve")
	}
	if _, ok := ReviewVerdictOK(`{"verdict":"bogus"}`); ok {
		t.Error("invalid verdict -> !ok")
	}
	if _, ok := ReviewVerdictOK(`{bad`); ok {
		t.Error("bad json -> !ok")
	}
}

func TestParseAndRenderResearch(t *testing.T) {
	args := map[string]any{
		"title":   "调研",
		"summary": "概述",
		"questions": []map[string]any{
			{"question": "Q1?", "answer": "A1"},
			{"question": ""},
		},
		"findings": []map[string]any{
			{"title": "发现1", "detail": "细节"},
		},
		"recommendation": "继续",
		"references":     []string{"ref1"},
		"follow_ups":     []string{"task1"},
	}
	doc, err := ParseResearch(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Questions) != 1 || len(doc.Findings) != 1 {
		t.Fatalf("parsed: q=%d f=%d", len(doc.Questions), len(doc.Findings))
	}
	if _, err := ParseResearch(map[string]any{}); err == nil {
		t.Fatal("empty summary should error")
	}
	if _, err := ParseResearch(map[string]any{"summary": "s"}); err == nil {
		t.Fatal("empty questions/findings should error")
	}

	raw := `{"title":"T","summary":"s","questions":[{"id":"q1","question":"Q","answer":"A"}],"findings":[{"id":"r1","title":"F","detail":"d"}],"recommendation":"go","references":["r"],"follow_ups":["f"]}`
	md := RenderResearchMarkdown(raw)
	for _, want := range []string{"T", "Q", "F", "go", "r", "f"} {
		if !strings.Contains(md, want) {
			t.Fatalf("research md missing %q: %s", want, md)
		}
	}
	if RenderResearchMarkdown(`{bad`) != `{bad` {
		t.Fatal("bad json raw")
	}
}

func TestClarifiedOpenQuestions(t *testing.T) {
	qs := ClarifiedOpenQuestions(`{"summary":"s","open_questions":[" q1 ","","q2"]}`)
	if len(qs) != 2 || qs[0] != "q1" {
		t.Fatalf("open questions: %+v", qs)
	}
	if ClarifiedOpenQuestions(`{bad`) != nil {
		t.Fatal("bad json -> nil")
	}
	if ClarifiedOpenQuestions(`{"summary":"s"}`) != nil {
		t.Fatal("empty -> nil")
	}
}
