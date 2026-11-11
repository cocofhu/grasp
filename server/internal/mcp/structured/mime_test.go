package structured

import "testing"

func TestTestCountsMore(t *testing.T) {
	// Prefer recounting cases when failed counter is zero.
	raw := `{"summary":"s","failed":0,"cases":[{"name":"a","status":"failed"},{"name":"b","status":"skipped"},{"name":"c","status":"skipped"}]}`
	if TestFailedCount(raw) != 1 {
		t.Fatalf("failed=%d", TestFailedCount(raw))
	}
	if TestSkippedCount(raw) != 2 {
		t.Fatalf("skipped=%d", TestSkippedCount(raw))
	}
}
