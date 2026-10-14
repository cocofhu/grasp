package engine

import "testing"

func TestEncodeBranchesVar(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want string
	}{
		{"git capture json string", `{"grasp":"feature/x"}`, `{"grasp":"feature/x"}`},
		{"node_complete decoded object", map[string]any{"grasp": "feature/home-light-theme-character"}, `{"grasp":"feature/home-light-theme-character"}`},
		{"string map", map[string]string{"web": "b1"}, `{"web":"b1"}`},
		{"drops blanks and non-strings", map[string]any{"web": "b1", "api": "", " ": "x", "n": 3}, `{"web":"b1"}`},
		{"legacy sprint string is rejected", "map[grasp:feature/x]", ""},
		{"empty", map[string]any{}, ""},
		{"nil", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodeBranchesVar(tc.in); got != tc.want {
				t.Fatalf("encodeBranchesVar(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
