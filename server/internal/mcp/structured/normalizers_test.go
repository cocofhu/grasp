package structured

import (
	"encoding/json"
	"testing"
)

func TestNormalizers(t *testing.T) {
	if severityRank("critical") != 0 || severityRank("high") != 1 || severityRank("medium") != 2 || severityRank("low") != 3 || severityRank("weird") != 4 {
		t.Error("severityRank mapping wrong")
	}
	if normSeverity("CRITICAL") != "critical" || normSeverity("bogus") != "medium" {
		t.Error("normSeverity wrong")
	}
	for _, v := range []string{"approve", "approve_with_comments", "request_changes", "reject"} {
		if !validVerdict(v) {
			t.Errorf("validVerdict(%q) should be true", v)
		}
	}
	if validVerdict("maybe") {
		t.Error("validVerdict(maybe) should be false")
	}
	if normTestStatus("pass") != "passed" || normTestStatus("skip") != "skipped" || normTestStatus("boom") != "failed" {
		t.Error("normTestStatus wrong")
	}
}

func TestCoerceAndAsHelpers(t *testing.T) {
	if coerceString(json.RawMessage(`"hi"`)) != "hi" {
		t.Error("coerceString string")
	}
	if coerceString(json.RawMessage(`{"title":"T"}`)) != "T" {
		t.Error("coerceString object.title")
	}
	if coerceString(json.RawMessage(`{"other":1}`)) == "" {
		t.Log("coerceString fallthrough")
	}
	if asString(42) != "42" || asString(nil) != "" || asString("x") != "x" {
		t.Errorf("asString wrong")
	}
}

func TestFlexStringsUnmarshal(t *testing.T) {
	var fs flexStrings
	if err := json.Unmarshal([]byte(`"solo"`), &fs); err != nil || len(fs) != 1 || fs[0] != "solo" {
		t.Errorf("bare string = %v, %v", fs, err)
	}
	fs = nil
	if err := json.Unmarshal([]byte(`["a","b"]`), &fs); err != nil || len(fs) != 2 {
		t.Errorf("array = %v, %v", fs, err)
	}
	fs = nil
	if err := json.Unmarshal([]byte(`[{"text":"c"}]`), &fs); err != nil || len(fs) != 1 || fs[0] != "c" {
		t.Errorf("object list = %v, %v", fs, err)
	}
	fs = nil
	if err := json.Unmarshal([]byte(`null`), &fs); err != nil || len(fs) != 0 {
		t.Errorf("null = %v, %v", fs, err)
	}
}
