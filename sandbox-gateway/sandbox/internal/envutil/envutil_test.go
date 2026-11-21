package envutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	t.Setenv("X_A", "")
	t.Setenv("X_B", "bee")
	if FirstNonEmpty("X_A", "X_B") != "bee" {
		t.Fatal("first non-empty")
	}
	if FirstNonEmpty("X_MISSING") != "" {
		t.Fatal("missing keys must yield empty")
	}
}

func TestEditing(t *testing.T) {
	env := []string{"A=1", "B=2"}
	if Get(env, "B") != "2" || Get(env, "Z") != "" {
		t.Fatal("Get")
	}
	env = SetIfEmpty(env, "B", "9")
	if Get(env, "B") != "2" {
		t.Fatal("SetIfEmpty must not overwrite")
	}
	env = SetIfEmpty(env, "C", "3")
	env = SetIfEmpty(env, "D", "")
	if Get(env, "C") != "3" || Get(env, "D") != "" {
		t.Fatal("SetIfEmpty insert / empty skip")
	}
	orig := []string{"A=1"}
	got := Upsert(orig, "A", "10")
	if Get(got, "A") != "10" || orig[0] != "A=1" {
		t.Fatal("Upsert must replace without mutating the caller's slice")
	}
}
