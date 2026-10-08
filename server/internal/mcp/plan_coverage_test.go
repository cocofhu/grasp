package mcp

import (
	"strings"
	"testing"
)

func TestPlanLeafIDs(t *testing.T) {
	plan := `{
		"goals":[
			{"id":"g1","title":"A","subgoals":[
				{"id":"g1.1","title":"a1","status":"done"},
				{"id":"g1.2","title":"a2","status":"pending"}
			]},
			{"id":"g2","title":"B","status":"pending"}
		]
	}`
	got := PlanLeafIDs(plan)
	want := []string{"g1.1", "g1.2", "g2"}
	if len(got) != len(want) {
		t.Fatalf("PlanLeafIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PlanLeafIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if PlanLeafIDs("") != nil {
		t.Fatal("empty plan should yield nil leaves")
	}
	if PlanLeafIDs(`{bad`) != nil {
		t.Fatal("malformed plan should yield nil leaves")
	}
	if len(PlanLeafIDs(`{"goals":[]}`)) != 0 {
		t.Fatal("empty goals should yield no leaves")
	}
}

func TestPlanCoverageOK(t *testing.T) {
	plan := `{"goals":[{"id":"g1","title":"A","subgoals":[{"id":"g1.1","title":"x"},{"id":"g1.2","title":"y"}]}]}`
	full := `{"summary":"ok","cases":[{"name":"a","status":"passed"},{"name":"b","status":"passed"}],"plan_coverage":[
		{"plan_id":"g1.1","passed":true,"evidence":"file a changed","cases":["a"]},
		{"plan_id":"g1.2","passed":true,"evidence":"file b changed","cases":["b"]}
	]}`

	t.Run("full pass", func(t *testing.T) {
		ok, reason := PlanCoverageOK(full, plan)
		if !ok || reason != "" {
			t.Fatalf("want pass, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("missing coverage field", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s"}`, plan)
		if ok || !strings.Contains(reason, "缺少 plan_coverage") {
			t.Fatalf("want missing coverage fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("missing leaf", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "未覆盖") || !strings.Contains(reason, "g1.2") {
			t.Fatalf("want missing leaf fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("unknown plan_id", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]},
			{"plan_id":"g9","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "未知") {
			t.Fatalf("want unknown id fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("duplicate plan_id", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]},
			{"plan_id":"g1.1","passed":true,"evidence":"ok2","cases":["a"]},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "重复") {
			t.Fatalf("want duplicate fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("passed false", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":false,"evidence":"ok"},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "未通过") {
			t.Fatalf("want passed=false fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("blank evidence", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"  "},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "evidence") {
			t.Fatalf("want blank evidence fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("missing case refs", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok"},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "g1.1 未关联任何用例") {
			t.Fatalf("want missing cases fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("blank case refs only", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["  "]},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "未关联任何用例") {
			t.Fatalf("want blank refs fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("referenced case skipped", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"},{"name":"e2e","status":"skipped","detail":"no time"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a","e2e"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "g1.2 关联的用例「e2e」未通过(skipped)") {
			t.Fatalf("want skipped ref fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("referenced case unknown", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["ghost"]}
		]}`, plan)
		if ok || !strings.Contains(reason, "「ghost」不存在") {
			t.Fatalf("want unknown ref fail, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("unreferenced skipped case allowed", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s","cases":[{"name":"a","status":"PASS"},{"name":"perf","status":"skipped","detail":"out of scope"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]},
			{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a"]}
		]}`, plan)
		if !ok {
			t.Fatalf("unreferenced skipped case should pass, got reason=%q", reason)
		}
	})
	t.Run("no plan fail-open", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s"}`, "")
		if !ok || reason != "" {
			t.Fatalf("no plan should pass, got ok=%v reason=%q", ok, reason)
		}
	})
	t.Run("empty leaves fail-open", func(t *testing.T) {
		ok, reason := PlanCoverageOK(`{"summary":"s"}`, `{"goals":[]}`)
		if !ok || reason != "" {
			t.Fatalf("empty leaves should pass, got ok=%v reason=%q", ok, reason)
		}
	})
}
